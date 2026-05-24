// Package pool manages the shared pioneer.ai API key pool.
//
// Users contribute their own pioneer keys to a shared pool. On each request
// the pool picks the active key with the least spend today that is still under
// its $1000/day cap. Spend is recorded after the request settles.
//
// Keys are stored AES-256-GCM encrypted at rest. The 32-byte secret is loaded
// from the POTLUCK_POOL_KEY_SECRET env var (hex or base64). If no secret is
// configured, keys are stored in plaintext — only safe for dev.
package pool

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/store"
)

// ErrNoKeys is returned when the pool has no active keys under their daily cap.
var ErrNoKeys = errors.New("pool: no active keys available")

// Manager is the pool key manager. Embed in handler structs or pass as a dep.
type Manager struct {
	q      *store.Queries
	secret []byte // 32 bytes for AES-256; nil = encryption disabled
}

// New constructs a Manager.
//
//   - q: store.Queries for DB access.
//   - secretHex: 64-char hex string (32 bytes). Empty string disables
//     encryption — only safe for dev without real keys in the DB.
func New(q *store.Queries, secretHex string) (*Manager, error) {
	m := &Manager{q: q}
	if secretHex != "" {
		// Accept both hex (64 chars) and base64 (44 chars) for convenience.
		var secret []byte
		var err error
		if len(secretHex) == 64 {
			secret, err = hex.DecodeString(secretHex)
		} else {
			secret, err = base64.StdEncoding.DecodeString(secretHex)
		}
		if err != nil {
			return nil, fmt.Errorf("pool: invalid secret: %w", err)
		}
		if len(secret) != 32 {
			return nil, fmt.Errorf("pool: secret must be 32 bytes, got %d", len(secret))
		}
		m.secret = secret
	}
	return m, nil
}

// HasHealthyKey returns true if the pool has at least one key that can serve
// a request right now. Used by PoolGate to short-circuit without decrypting.
func (m *Manager) HasHealthyKey(ctx context.Context) bool {
	_, err := m.q.PickPoolKeyV2(ctx)
	return err == nil
}

// Pick selects the best key for a request using the v2 picker (health-aware,
// $10 buffer, max_micros cap). Returns ErrNoKeys when the pool has no
// eligible keys — the caller should surface this as a 503 to the user.
//
// Deprecated for new code paths: prefer PickForUser, which routes the
// request through the user's own key first if they have private
// reservation. Pick remains for callers without a user context.
func (m *Manager) Pick(ctx context.Context) (*Selection, error) {
	key, err := m.q.PickPoolKeyV2(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoKeys
		}
		return nil, fmt.Errorf("pool: pick key: %w", err)
	}
	plaintext, err := m.Decrypt(key.KeyCiphertext)
	if err != nil {
		return nil, fmt.Errorf("pool: decrypt key %s: %w", key.ID, err)
	}
	return &Selection{keyID: key.ID, plaintext: plaintext}, nil
}

// PickForUser is the user-aware variant of Pick. It prefers a key owned
// by the requesting user that still has private reservation capacity
// (max_micros > shared_micros AND today_micros < max_micros). This makes
// "private reservation" actually do what its name implies: the owner's
// spend goes through their own key first so the reconciler attributes
// it as private, not shared.
//
// Falls back to the standard pool order when the user has no eligible
// own-key. Returns ErrNoKeys when the pool has no eligible keys.
func (m *Manager) PickForUser(ctx context.Context, userID string) (*Selection, error) {
	if userID == "" {
		return m.Pick(ctx)
	}
	// Try the user's own key with private budget first.
	key, err := m.q.PickOwnKeyWithPrivateBudget(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		// No eligible own-key; fall back to standard pool ordering.
		return m.Pick(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("pool: pick key for user %s: %w", userID, err)
	}
	plaintext, err := m.Decrypt(key.KeyCiphertext)
	if err != nil {
		return nil, fmt.Errorf("pool: decrypt key %s: %w", key.ID, err)
	}
	return &Selection{keyID: key.ID, plaintext: plaintext}, nil
}

// RecordSpend records spend for the key used in a request.
// Call this after the upstream call settles (use context.Background() — the
// request context may already be canceled).
func (m *Manager) RecordSpend(ctx context.Context, sel *Selection, amountMicros int64) error {
	if sel.keyID == "" {
		return nil
	}
	now := time.Now().Unix()
	return m.q.RecordPoolKeySpend(ctx, store.RecordPoolKeySpendParams{
		TodayDate:   time.Now().UTC().Unix() / 86400,
		TodayMicros: amountMicros,
		LastUsedAt:  sql.NullInt64{Int64: now, Valid: true},
		ID:          sel.keyID,
	})
}

// Encrypt encrypts a plaintext pioneer API key for storage.
// Returns base64url(nonce || ciphertext).
func (m *Manager) Encrypt(plaintext string) (string, error) {
	if m.secret == nil {
		// Dev mode: store plaintext with a marker prefix. NOT for production.
		return "plain:" + plaintext, nil
	}
	block, err := aes.NewCipher(m.secret)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a stored ciphertext.
func (m *Manager) Decrypt(ciphertext string) (string, error) {
	if len(ciphertext) >= 6 && ciphertext[:6] == "plain:" {
		return ciphertext[6:], nil // dev-mode plaintext
	}
	if m.secret == nil {
		return "", errors.New("pool: no encryption secret configured but ciphertext is not plaintext-marked")
	}
	raw, err := base64.URLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("pool: decode: %w", err)
	}
	block, err := aes.NewCipher(m.secret)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("pool: ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("pool: decrypt: %w", err)
	}
	return string(plain), nil
}

// Fingerprint returns the first 16 hex chars of SHA-256(plaintext).
// Used for dedup without decrypting.
func Fingerprint(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])[:16]
}

// Selection is the result of a Pick call. Pass it to RecordSpend after the
// upstream request settles.
type Selection struct {
	keyID     string
	plaintext string
}

// APIKey returns the decrypted pioneer API key for use in HTTP headers.
func (s *Selection) APIKey() string { return s.plaintext }

// KeyID returns the DB row ID of the selected key.
func (s *Selection) KeyID() string { return s.keyID }
