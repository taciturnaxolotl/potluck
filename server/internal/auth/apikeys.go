// Package auth — API key generation, parsing, validation.
//
// Format: pot_<word>_<18-char-base62>_<5-char-checksum>
//
// Example: pot_cedar_KJ3mN8pQwR5vX2yZ4b_9xK2m
//
// The full plaintext key is shown to the user exactly once at creation.
// The DB stores only sha256(plaintext) (UNIQUE), the mnemonic word, and
// the 5-char checksum (for masked display). Plaintext never persists.
//
// Validation is two-step:
//  1. Parse format + verify checksum (no DB; cheap fast-fail).
//  2. Look up sha256(plaintext) in api_keys (excludes revoked rows).
//
// Both steps use constant-time comparison.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

const (
	keyPrefix       = "pot_"
	keyEntropyChars = 18 // ~107 bits of base62
	keyChecksumLen  = 5
)

// base62 alphabet (numbers, upper, lower).
const b62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// TestKey is the canonical fixture key. Use it in tests, docs, and the
// SDK README. The checksum is computed at init() to keep this stable
// across refactors without hand-computing it.
//
// Never accept this key in production code paths — middleware should
// short-circuit it to an obvious error.
var TestKey = "pot_test_000000000000000000_" + computeChecksum("pot_test_000000000000000000")

// errors -------------------------------------------------------------

var (
	ErrKeyMalformed = errors.New("api key malformed")
	ErrKeyChecksum  = errors.New("api key checksum mismatch")
	ErrKeyWord      = errors.New("api key word not in dictionary")
)

// GeneratedKey holds the result of NewKey: the plaintext to show the user
// and the storable hash + word + last4 to put in the DB.
type GeneratedKey struct {
	Plaintext string // pot_<word>_<entropy>_<checksum> — show ONCE.
	Hash      string // sha256 hex; goes in api_keys.key_hash
	Word      string // word component (already in plaintext, also stored)
	Last4     string // checksum component (mnemonic for masked display)
}

// NewKey mints a fresh API key. Pure function; caller persists.
func NewKey() (GeneratedKey, error) {
	word, err := pickWord()
	if err != nil {
		return GeneratedKey{}, err
	}
	entropy, err := randomBase62(keyEntropyChars)
	if err != nil {
		return GeneratedKey{}, err
	}
	payload := keyPrefix + word + "_" + entropy
	cs := computeChecksum(payload)
	plaintext := payload + "_" + cs
	return GeneratedKey{
		Plaintext: plaintext,
		Hash:      hashKey(plaintext),
		Word:      word,
		Last4:     cs,
	}, nil
}

// HashKey is exported for callers that have a plaintext key and need its
// storage hash (e.g. the lookup path in middleware).
func HashKey(plaintext string) string { return hashKey(plaintext) }

func hashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// ParsedKey is the result of a successful checksum-level parse. The DB
// lookup happens separately (with the Hash) so callers can fan out the
// failure paths.
type ParsedKey struct {
	Word     string
	Entropy  string
	Checksum string
	Hash     string
}

// ParseKey validates the format and checksum of a plaintext key.
//
// Constant-time-friendly: the checksum compare uses subtle.ConstantTimeCompare,
// and the function does not branch on which character index differs. It does
// branch on overall format validity (length, prefix, separators) — by design,
// since those failures are public information.
func ParseKey(plaintext string) (ParsedKey, error) {
	if !strings.HasPrefix(plaintext, keyPrefix) {
		return ParsedKey{}, ErrKeyMalformed
	}
	rest := plaintext[len(keyPrefix):]
	parts := strings.Split(rest, "_")
	if len(parts) != 3 {
		return ParsedKey{}, ErrKeyMalformed
	}
	word, entropy, checksum := parts[0], parts[1], parts[2]
	if word == "" || len(entropy) != keyEntropyChars || len(checksum) != keyChecksumLen {
		return ParsedKey{}, ErrKeyMalformed
	}
	if !validBase62(entropy) || !validBase62(checksum) {
		return ParsedKey{}, ErrKeyMalformed
	}
	if _, ok := keyWordIndex[word]; !ok && word != "test" {
		return ParsedKey{}, ErrKeyWord
	}
	want := computeChecksum(keyPrefix + word + "_" + entropy)
	if subtle.ConstantTimeCompare([]byte(want), []byte(checksum)) != 1 {
		return ParsedKey{}, ErrKeyChecksum
	}
	return ParsedKey{
		Word:     word,
		Entropy:  entropy,
		Checksum: checksum,
		Hash:     hashKey(plaintext),
	}, nil
}

// MaskKey produces a UI-safe representation of a plaintext key for display
// after the one-time reveal. Returns "" on a malformed input rather than
// echoing back something that looks legit.
//
//	pot_cedar_••••••••••••••••••_9xK2m
func MaskKey(plaintext string) string {
	p, err := ParseKey(plaintext)
	if err != nil {
		return ""
	}
	return keyPrefix + p.Word + "_" + strings.Repeat("•", keyEntropyChars) + "_" + p.Checksum
}

// computeChecksum returns the first 5 base62 chars of sha256(payload),
// where payload is everything before the trailing "_<checksum>".
func computeChecksum(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	// First 3 bytes (24 bits) interpreted as big-endian unsigned, encoded
	// in base62 — yields up to 5 chars; pad with leading '0' if shorter.
	n := big.NewInt(0).SetBytes(sum[:3])
	out := encodeBase62(n)
	if len(out) < keyChecksumLen {
		out = strings.Repeat("0", keyChecksumLen-len(out)) + out
	}
	return out[:keyChecksumLen]
}

func encodeBase62(n *big.Int) string {
	if n.Sign() == 0 {
		return "0"
	}
	base := big.NewInt(62)
	mod := new(big.Int)
	var b strings.Builder
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		b.WriteByte(b62[mod.Int64()])
	}
	// reverse
	r := []byte(b.String())
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

func randomBase62(n int) (string, error) {
	out := make([]byte, n)
	max := big.NewInt(62)
	for i := range n {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("random base62: %w", err)
		}
		out[i] = b62[v.Int64()]
	}
	return string(out), nil
}

func validBase62(s string) bool {
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		default:
			return false
		}
	}
	return true
}

func pickWord() (string, error) {
	max := big.NewInt(int64(len(keyWords)))
	v, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("random word: %w", err)
	}
	return keyWords[v.Int64()], nil
}
