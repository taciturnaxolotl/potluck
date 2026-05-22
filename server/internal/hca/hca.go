// Package hca is a thin client for Hack Club Auth.
//
// HCA is a standard OAuth 2.0 + OIDC provider. We use the bare authorization
// code flow (no PKCE since potluck has a real backend that holds the
// client secret). The full docs live at:
//
//	https://identity.hackclub.com/docs/oauth-guide
//	https://identity.hackclub.com/docs/api
//
// Scope guidance: stick to the community scopes (`openid email name
// slack_id verification_status`) unless we explicitly need more. Anything
// beyond that requires HQ approval.
package hca

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client wraps the small slice of HCA's surface we actually use:
// AuthorizeURL, ExchangeCode, and Me.
type Client struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string

	HTTP *http.Client
}

// New builds a client with a 10s default HTTP timeout.
func New(baseURL, clientID, clientSecret, redirectURL, scopes string) *Client {
	return &Client{
		BaseURL:      strings.TrimRight(baseURL, "/"),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
		HTTP:         &http.Client{Timeout: 10 * time.Second},
	}
}

// NewState returns a 32-byte hex string suitable for the OAuth `state`
// parameter. The caller is expected to bind this to the user's pre-auth
// session (we use a short-lived cookie) and verify it matches on callback.
func NewState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// AuthorizeURL builds the URL the browser should be redirected to. The
// state value is propagated back on the callback for CSRF protection.
func (c *Client) AuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", c.RedirectURL)
	q.Set("response_type", "code")
	q.Set("scope", c.Scopes)
	q.Set("state", state)
	return c.BaseURL + "/oauth/authorize?" + q.Encode()
}

// TokenResponse is the subset of HCA's token-exchange response we care
// about. The refresh_token is captured so we can roll over expired access
// tokens without bouncing the user back through authorize, but we don't
// persist it anywhere yet — see TODO in callback handler.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// ExchangeCode swaps an authorization code for an access token.
func (c *Client) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"client_id":     c.ClientID,
		"client_secret": c.ClientSecret,
		"redirect_uri":  c.RedirectURL,
		"code":          code,
		"grant_type":    "authorization_code",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/oauth/token", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hca token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return nil, statusErr(resp.StatusCode, resp.Body)
	}
	var out TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("hca token decode: %w", err)
	}
	return &out, nil
}

// Identity is the slice of `/api/v1/me` we read. Fields are nullable on
// purpose; HCA only returns what the granted scopes allow.
//
// Note: HCA's wire format uses `primary_email` (not `email`) and has no
// flat `name` field — we compose Name from first_name + last_name.
type Identity struct {
	ID                 string `json:"id"` // ident!xxxxx
	Email              string `json:"primary_email"`
	Name               string `json:"-"` // composed from FirstName + LastName
	FirstName          string `json:"first_name"`
	LastName           string `json:"last_name"`
	SlackID            string `json:"slack_id"`
	VerificationStatus string `json:"verification_status"`
	YSWSEligible       bool   `json:"ysws_eligible"`
}

// meResponse is HCA's actual on-the-wire shape: `{ "identity": {...},
// "scopes": [...] }`. We unwrap it inside Me() before returning Identity
// so the caller never has to know.
type meResponse struct {
	Identity Identity `json:"identity"`
	Scopes   []string `json:"scopes"`
}

// Me calls /api/v1/me with the supplied access token. HCA returns the
// identity wrapped under an `identity` key alongside the granted scopes;
// we unwrap and compose a display Name from the first/last fields so
// callers get a flat struct.
func (c *Client) Me(ctx context.Context, accessToken string) (*Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hca me: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return nil, statusErr(resp.StatusCode, resp.Body)
	}
	var wrap meResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrap); err != nil {
		return nil, fmt.Errorf("hca me decode: %w", err)
	}
	ident := wrap.Identity
	ident.Name = strings.TrimSpace(ident.FirstName + " " + ident.LastName)
	return &ident, nil
}

func statusErr(status int, body interface{ Read(p []byte) (int, error) }) error {
	buf := make([]byte, 1024)
	n, _ := body.Read(buf)
	return fmt.Errorf("hca: HTTP %d: %s", status, strings.TrimSpace(string(buf[:n])))
}
