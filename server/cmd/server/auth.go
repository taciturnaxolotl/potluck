package main

import (
	"database/sql"
	"net/http"
	"time"

	"charm.land/log/v2"
	"github.com/google/uuid"

	"github.com/taciturnaxolotl/potluck/internal/auth"
	"github.com/taciturnaxolotl/potluck/internal/hca"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

// hcaStateCookie is the short-lived cookie that ties an outgoing
// authorize request to the eventual callback. It exists for ~10 minutes
// and is cleared as soon as we read it.
const hcaStateCookie = "potluck_hca_state"

// hcaLoginHandler kicks off the OAuth flow. We mint a CSRF state value,
// drop it in a short-lived cookie, then 302 the user to HCA.
//
// HCA isn't configured in dev by default; in that case we render a
// helpful 503 instead of redirecting to a broken authorize URL.
func hcaLoginHandler(client *hca.Client, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			http.Error(w, "Hack Club Auth is not configured on this server.", http.StatusServiceUnavailable)
			return
		}
		state, err := hca.NewState()
		if err != nil {
			log.Error("hca: state mint", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     hcaStateCookie,
			Value:    state,
			Path:     "/auth/callback",
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   600, // 10 minutes
		})
		http.Redirect(w, r, client.AuthorizeURL(state), http.StatusFound)
	}
}

// hcaCallbackHandler completes the flow. On success we:
//
//  1. Verify the state cookie matches the `state` query param.
//  2. Exchange the code for an access token.
//  3. Fetch the user's identity from /api/v1/me.
//  4. Upsert by HCA id; mint a potluck session cookie.
//  5. Send the user to /dashboard.
//
// Failures redirect back to /?auth_error=<code> so the splash can show a
// friendly hint without leaking detail.
func hcaCallbackHandler(client *hca.Client, q *store.Queries, authSvc *auth.Service, sessionTTL time.Duration, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			http.Error(w, "Hack Club Auth is not configured.", http.StatusServiceUnavailable)
			return
		}

		// 1. State check.
		stateCookie, err := r.Cookie(hcaStateCookie)
		if err != nil || stateCookie.Value == "" {
			loginRedirect(w, r, "missing_state")
			return
		}
		// One-shot: clear regardless of success.
		http.SetCookie(w, &http.Cookie{
			Name:     hcaStateCookie,
			Value:    "",
			Path:     "/auth/callback",
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})

		got := r.URL.Query().Get("state")
		if got == "" || got != stateCookie.Value {
			log.Warn("hca callback: state mismatch")
			loginRedirect(w, r, "bad_state")
			return
		}

		// 2. Code exchange.
		code := r.URL.Query().Get("code")
		if code == "" {
			loginRedirect(w, r, "no_code")
			return
		}
		token, err := client.ExchangeCode(r.Context(), code)
		if err != nil {
			log.Error("hca: exchange code", "err", err)
			loginRedirect(w, r, "exchange_failed")
			return
		}

		// 3. Identity lookup.
		ident, err := client.Me(r.Context(), token.AccessToken)
		if err != nil {
			log.Error("hca: /me lookup", "err", err)
			loginRedirect(w, r, "me_failed")
			return
		}
		if ident.ID == "" {
			log.Warn("hca: empty identity id", "ident", ident)
			loginRedirect(w, r, "no_identity")
			return
		}

		// 4. Upsert + session.
		now := time.Now().Unix()
		display := ident.Name
		if display == "" {
			display = ident.Email
		}
		user, err := q.UpsertUserByHCAID(r.Context(), store.UpsertUserByHCAIDParams{
			ID:                 uuid.NewString(),
			Email:              ident.Email,
			DisplayName:        display,
			HcaID:              nullStr(ident.ID),
			SlackID:            nullStr(ident.SlackID),
			VerificationStatus: nullStr(ident.VerificationStatus),
			CreatedAt:          now,
		})
		if err != nil {
			log.Error("hca: upsert user", "err", err, "hca_id", ident.ID)
			loginRedirect(w, r, "user_upsert_failed")
			return
		}

		tok, err := authSvc.IssueSession(r.Context(), user.ID)
		if err != nil {
			log.Error("hca: mint session", "err", err, "user_id", user.ID)
			loginRedirect(w, r, "session_failed")
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     auth.CookieName,
			Value:    tok,
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(sessionTTL),
		})

		log.Info("hca: signed in", "user_id", user.ID, "hca_id", ident.ID, "email", ident.Email)
		// 5. Off you go.
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	}
}

// loginRedirect bounces the user back to the splash with a short,
// non-leaking error code in the query string. The splash maps the code
// to a friendly hint; full detail stays in server logs.
func loginRedirect(w http.ResponseWriter, r *http.Request, code string) {
	log.Warn("hca: login redirect", "code", code, "path", r.URL.Path)
	http.Redirect(w, r, "/?auth_error="+code, http.StatusFound)
}

// nullStr lifts an empty-or-not string into a sql.NullString. We use this
// so optional HCA fields end up as NULL in SQLite rather than empty
// strings, which makes "did the user have a slack id?" queries less
// surprising.
func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
