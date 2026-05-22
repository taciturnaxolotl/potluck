// Package money handles all monetary values as int64 micros.
//
// 1 USD = 1,000,000 micros. Floats anywhere in the spend path are a bug.
// All ledger math, balance checks, and provider pricing live in this unit.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Micros is a signed amount of USD denominated in millionths.
//
//	1 USD       = 1_000_000 micros
//	1 cent      =    10_000 micros
//	1 mill      =     1_000 micros
//	1 micro     =         1 micros
type Micros int64

const (
	PerUSD  Micros = 1_000_000
	PerCent Micros = 10_000
)

// FromUSD converts a whole-USD integer to micros. Use this only for constants
// and config defaults; never for ingesting values that originated as floats.
func FromUSD(usd int64) Micros { return Micros(usd) * PerUSD }

// FromCents converts an integer cent count to micros.
func FromCents(cents int64) Micros { return Micros(cents) * PerCent }

// ParseUSD parses a decimal USD string ("1.23", "0.000125") into micros.
// Up to 6 fractional digits are preserved; anything beyond is truncated.
// Returns an error on malformed input. Never goes through float64.
func ParseUSD(s string) (Micros, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("money: empty string")
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	intPart, fracPart, hasFrac := s, "", false
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart, hasFrac = s[:i], s[i+1:], true
	}
	if intPart == "" && !hasFrac {
		return 0, fmt.Errorf("money: %q is not a number", s)
	}
	if intPart == "" {
		intPart = "0"
	}
	// After sign stripping, no further sign is allowed in either part.
	if intPart[0] == '+' || intPart[0] == '-' {
		return 0, fmt.Errorf("money: stray sign in %q", s)
	}
	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money: bad integer part %q: %w", intPart, err)
	}
	var frac int64
	if hasFrac {
		// pad/truncate to exactly 6 digits
		switch {
		case len(fracPart) < 6:
			fracPart += strings.Repeat("0", 6-len(fracPart))
		case len(fracPart) > 6:
			fracPart = fracPart[:6]
		}
		frac, err = strconv.ParseInt(fracPart, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("money: bad fractional part: %w", err)
		}
	}
	v := whole*int64(PerUSD) + frac
	if neg {
		v = -v
	}
	return Micros(v), nil
}

// USDString formats a Micros amount as a decimal USD string with up to 6
// fractional digits, trimming trailing zeros after the cent place. Always
// includes at least cents ("1.00", "0.000125", "-0.50").
func (m Micros) USDString() string {
	neg := m < 0
	v := m
	if neg {
		v = -v
	}
	whole := int64(v) / int64(PerUSD)
	frac := int64(v) % int64(PerUSD)
	out := fmt.Sprintf("%d.%06d", whole, frac)
	// Trim trailing zeros below the cent place but keep at least 2 digits.
	dot := strings.IndexByte(out, '.')
	end := len(out)
	for end > dot+3 && out[end-1] == '0' {
		end--
	}
	out = out[:end]
	if neg {
		out = "-" + out
	}
	return out
}

func (m Micros) String() string { return "$" + m.USDString() }
