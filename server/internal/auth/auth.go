// Package auth handles session-cookie authentication.
//
// Sessions are opaque random tokens. Only their SHA-256 hash is persisted;
// the plaintext lives in the user's cookie. On lookup we hash, compare, and
// rotate the last_used timestamp.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

const CookieName = "potluck_session"

type ctxKey int

const userKey ctxKey = 1

// Service exposes the auth surface that handlers depend on.
type Service struct {
	q   *store.Queries
	ttl time.Duration
}

func New(q *store.Queries, ttl time.Duration) *Service {
	return &Service{q: q, ttl: ttl}
}

// IssueSession mints a new session for userID and returns the plaintext
// token to set on the client cookie.
func (s *Service) IssueSession(ctx context.Context, userID, ip, userAgent string) (string, error) {
	tok, err := randomToken(32)
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	_, err = s.q.CreateSession(ctx, store.CreateSessionParams{
		ID:         hashToken(tok),
		UserID:     userID,
		CreatedAt:  now,
		ExpiresAt:  now + int64(s.ttl.Seconds()),
		LastUsedAt: now,
		Ip:         nullStr(ip),
		UserAgent:  nullStr(userAgent),
	})
	if err != nil {
		return "", err
	}
	return tok, nil
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// Middleware loads the user from the session cookie and stashes it on the
// request context. Requests without a valid session pass through unauthenticated;
// it's up to the handler to require auth.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		sess, err := s.q.GetSession(r.Context(), store.GetSessionParams{
			ID:        hashToken(c.Value),
			ExpiresAt: time.Now().Unix(),
		})
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		_ = s.q.TouchSession(r.Context(), store.TouchSessionParams{
			LastUsedAt: time.Now().Unix(),
			ID:         sess.ID,
		})
		u, err := s.q.GetUserByID(r.Context(), sess.UserID)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), userKey, &u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext returns the authenticated user, if any.
func UserFromContext(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(userKey).(*store.User)
	return u, ok
}

// WithUser returns ctx augmented with u. Used by both the cookie middleware
// here and the bearer middleware in api/middleware so they share one
// context key — handlers don't care which path authenticated the request.
func WithUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// Require is a middleware that 401s anonymous requests.
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFromContext(r.Context()); !ok {
			http.Error(w, `{"error":{"code":"unauthenticated","message":"login required"}}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// HashToken is the exported form for callers outside the auth package that
// need to resolve a plaintext cookie to its DB id (e.g. session listing).
func HashToken(tok string) string { return hashToken(tok) }

// RevokeSession deletes the session identified by the plaintext token.
// Used by the logout handler; silently succeeds if the session is already
// gone.
func (s *Service) RevokeSession(ctx context.Context, plaintextToken string) error {
	return s.q.DeleteSession(ctx, hashToken(plaintextToken))
}
