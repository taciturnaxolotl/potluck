// Package middleware holds the shared HTTP pipeline used by both
// /api/* (cookie-auth) and /v1/* (bearer-auth) surfaces.
//
// The authentication step is the only piece that differs between the two —
// CookieAuth and BearerAuth both deposit a *store.User and (optionally) a
// *store.ApiKey on the request context, so downstream middleware
// (balance, rate limit, spend) can stay identical.
package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/taciturnaxolotl/potluck/internal/auth"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

// ---- context plumbing --------------------------------------------------

type ctxKey int

const apiKeyKey ctxKey = iota

// UserFromContext returns the authenticated user, if any. Delegates to the
// auth package so cookie + bearer flows share one context key.
func UserFromContext(ctx context.Context) (*store.User, bool) {
	return auth.UserFromContext(ctx)
}

// APIKeyFromContext returns the API key the request was authenticated with,
// if it came in via the bearer flow. nil for cookie auth.
func APIKeyFromContext(ctx context.Context) (*store.ApiKey, bool) {
	k, ok := ctx.Value(apiKeyKey).(*store.ApiKey)
	return k, ok
}

func withAPIKey(ctx context.Context, k *store.ApiKey) context.Context {
	return context.WithValue(ctx, apiKeyKey, k)
}

// ---- auth: cookie (web) ------------------------------------------------

// CookieAuth resolves the cookie-session user. Anonymous requests pass
// through unauthenticated; the per-route Require gate enforces presence.
func CookieAuth(svc *auth.Service) func(http.Handler) http.Handler {
	return svc.Middleware
}

// ---- auth: bearer (v1) -------------------------------------------------

// BearerAuth resolves an API key from the Authorization header. The
// shape of failure responses is the caller's job (see internal/api/v1's
// errors.go); this middleware only writes a generic 401 when there is
// no key or when validation fails.
func BearerAuth(q *store.Queries, errResp ErrorResponder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok, ok := bearerToken(r)
			if !ok {
				errResp(w, http.StatusUnauthorized, "invalid_api_key", "missing bearer token")
				return
			}

			// 1. Cheap: format + checksum.
			if _, err := auth.ParseKey(tok); err != nil {
				errResp(w, http.StatusUnauthorized, "invalid_api_key", "malformed api key")
				return
			}

			// 2. DB lookup.
			row, err := q.GetAPIKeyByHash(r.Context(), auth.HashKey(tok))
			if err != nil {
				errResp(w, http.StatusUnauthorized, "invalid_api_key", "unknown api key")
				return
			}

			user, err := q.GetUserByID(r.Context(), row.UserID)
			if err != nil {
				errResp(w, http.StatusUnauthorized, "invalid_api_key", "owning user gone")
				return
			}

			// Debounced touch — see AGENTS.md "Public API".
			touchAPIKey(r.Context(), q, &row)

			ctx := auth.WithUser(r.Context(), &user)
			ctx = withAPIKey(ctx, &row)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) < len("Bearer ") || h[:7] != "Bearer " {
		return "", false
	}
	return h[7:], true
}

// debounce state for last_used_at writes. SQLite's WAL doesn't love a
// write per request — the AGENTS.md mandate is "at most once per minute".
var (
	touchMu   sync.Mutex
	touchSeen = map[string]int64{} // api_key id -> unix ts of last write
)

const touchInterval = 60 // seconds

func touchAPIKey(ctx context.Context, q *store.Queries, k *store.ApiKey) {
	now := time.Now().Unix()
	touchMu.Lock()
	last := touchSeen[k.ID]
	if now-last < touchInterval {
		touchMu.Unlock()
		return
	}
	touchSeen[k.ID] = now
	touchMu.Unlock()
	_ = q.TouchAPIKey(ctx, store.TouchAPIKeyParams{
		LastUsedAt: nullable(now),
		ID:         k.ID,
	})
}

// ---- require auth ------------------------------------------------------

// Require fails closed: requests without a user in context get an error
// in the shape the surface chose (cookie vs OpenAI envelope).
func Require(errResp ErrorResponder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := UserFromContext(r.Context()); !ok {
				errResp(w, http.StatusUnauthorized, "unauthenticated", "login required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---- active user gate --------------------------------------------------

// RequireActive blocks banned and waitlisted users. Mount after Require so
// the user is already in context.
func RequireActive(errResp ErrorResponder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			switch u.Status {
			case "banned":
				errResp(w, http.StatusForbidden, "banned", "your account has been banned")
			case "waitlisted":
				errResp(w, http.StatusForbidden, "waitlisted", "your account is pending approval")
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

// ---- pool gate ---------------------------------------------------------

// GateResult carries a rejection reason from CheckPoolGate.
type GateResult struct {
	Status  int
	Code    string
	Message string
}

// CheckPoolGate performs the pool-health and per-user budget checks.
// Returns nil when the request may proceed, or a *GateResult describing the
// rejection. u must not be nil; the caller is responsible for ensuring a user
// is present in context before calling this.
func CheckPoolGate(ctx context.Context, q *store.Queries, poolAvailable func(context.Context) bool, u *store.User) *GateResult {
	// 1. Pool health — can we even route a request?
	if !poolAvailable(ctx) {
		return &GateResult{http.StatusServiceUnavailable, "no_pool_keys", "no active pool keys available"}
	}

	// 2. Per-user budget check.
	now := time.Now().UTC()
	day := now.Unix() / 86400
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Unix()

	allowRow, err := q.GetUserDailyAllowance(ctx, store.GetUserDailyAllowanceParams{
		UserID: u.ID,
		Day:    day,
	})
	if err != nil {
		// No allowance row yet — reconciler hasn't set one. Default: allow.
		return nil
	}

	liveSpend, err := q.GetUserLiveSpendToday(ctx, store.GetUserLiveSpendTodayParams{
		AttributedUserID: sql.NullString{String: u.ID, Valid: true},
		PioneerCreatedAt: dayStart,
	})
	if err != nil {
		// Can't read spend — allow rather than incorrectly block.
		return nil
	}

	sharedSpent := toI64(liveSpend.SharedSpentMicros)
	privateSpent := toI64(liveSpend.PrivateSpentMicros)

	var privateReserved int64
	privateRows, err := q.ListPoolKeysForUser(ctx, u.ID)
	if err == nil {
		for _, k := range privateRows {
			if k.Active == 1 && !k.RevokedAt.Valid {
				privateReserved += k.MaxMicros - k.SharedMicros
			}
		}
	}

	privateRemaining := privateReserved - privateSpent
	privateOverflow := int64(0)
	if privateSpent > privateReserved {
		privateOverflow = privateSpent - privateReserved
	}
	sharedRemaining := allowRow.SharedAllowanceMicros - sharedSpent - privateOverflow

	if sharedRemaining > 0 || privateRemaining > 0 {
		return nil
	}
	return &GateResult{
		http.StatusPaymentRequired,
		"insufficient_funds",
		"your shared allowance and private reservation for today are both exhausted",
	}
}

// PoolGate replaces BalanceGate. Checks pool health and per-user budget.
// poolAvailable should return true if at least one healthy key can serve requests.
func PoolGate(q *store.Queries, poolAvailable func(ctx context.Context) bool, errResp ErrorResponder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok {
				errResp(w, http.StatusUnauthorized, "unauthenticated", "login required")
				return
			}
			if gr := CheckPoolGate(r.Context(), q, poolAvailable, u); gr != nil {
				errResp(w, gr.Status, gr.Code, gr.Message)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}


// ---- per-user rate limiter --------------------------------------------

// toI64 unboxes the interface{} sqlc emits for aggregate columns.
func toI64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	}
	return 0
}

// RateLimit is a per-user token bucket. The map is bounded by the active
// user set — keys are GC'd by the periodic sweep.
func RateLimit(rps float64, burst int, errResp ErrorResponder) func(http.Handler) http.Handler {
	limiters := &userLimiters{m: map[string]*rate.Limiter{}, rps: rps, burst: burst}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if !limiters.get(u.ID).Allow() {
				errResp(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type userLimiters struct {
	mu    sync.Mutex
	m     map[string]*rate.Limiter
	rps   float64
	burst int
}

func (u *userLimiters) get(id string) *rate.Limiter {
	u.mu.Lock()
	defer u.mu.Unlock()
	l, ok := u.m[id]
	if !ok {
		l = rate.NewLimiter(rate.Limit(u.rps), u.burst)
		u.m[id] = l
	}
	return l
}

// ---- error response shape ---------------------------------------------

// ErrorResponder writes a surface-appropriate error body. The /api/* surface
// uses {"error":{"code","message"}}; /v1/* uses the OpenAI envelope. The
// caller injects the right one per route tree.
type ErrorResponder func(w http.ResponseWriter, status int, code, message string)

// ---- utilities --------------------------------------------------------

func nullable(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }
