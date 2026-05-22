package auth

import (
	"strings"
	"testing"
)

func TestNewKeyRoundTrip(t *testing.T) {
	for range 20 {
		k, err := NewKey()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(k.Plaintext, "pot_") {
			t.Errorf("missing prefix: %q", k.Plaintext)
		}
		p, err := ParseKey(k.Plaintext)
		if err != nil {
			t.Fatalf("ParseKey(%q) failed: %v", k.Plaintext, err)
		}
		if p.Word != k.Word {
			t.Errorf("word mismatch: %q vs %q", p.Word, k.Word)
		}
		if p.Checksum != k.Last4 {
			t.Errorf("checksum mismatch: %q vs %q", p.Checksum, k.Last4)
		}
		if p.Hash != k.Hash {
			t.Errorf("hash mismatch")
		}
	}
}

func TestParseKeyRejects(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"no prefix":        "cedar_KJ3mN8pQwR5vX2yZ4b_9xK2m",
		"missing pieces":   "pot_cedar_9xK2m",
		"short entropy":    "pot_cedar_short_9xK2m",
		"bad word":         "pot_zzzzznever_KJ3mN8pQwR5vX2yZ4b_9xK2m",
		"bad checksum":     "pot_cedar_KJ3mN8pQwR5vX2yZ4b_00000",
		"non-base62":       "pot_cedar_KJ3mN8pQ!R5vX2yZ4b_9xK2m",
		"trailing garbage": "pot_cedar_KJ3mN8pQwR5vX2yZ4b_9xK2mEXTRA",
	}
	for name, in := range cases {
		if _, err := ParseKey(in); err == nil {
			t.Errorf("%s: ParseKey(%q) should have failed", name, in)
		}
	}
}

func TestTestKeyValid(t *testing.T) {
	if _, err := ParseKey(TestKey); err != nil {
		t.Fatalf("TestKey %q must validate: %v", TestKey, err)
	}
}

func TestMaskKey(t *testing.T) {
	k, _ := NewKey()
	masked := MaskKey(k.Plaintext)
	if !strings.Contains(masked, k.Word) {
		t.Errorf("masked key should keep word: %q", masked)
	}
	if !strings.Contains(masked, k.Last4) {
		t.Errorf("masked key should keep checksum: %q", masked)
	}
	if strings.Contains(masked, k.Plaintext[len("pot_")+len(k.Word)+1:len("pot_")+len(k.Word)+1+keyEntropyChars]) {
		t.Errorf("masked key leaks entropy: %q", masked)
	}
	if MaskKey("garbage") != "" {
		t.Errorf("garbage should mask to empty string")
	}
}

func TestChecksumDeterministic(t *testing.T) {
	a := computeChecksum("pot_cedar_KJ3mN8pQwR5vX2yZ4b")
	b := computeChecksum("pot_cedar_KJ3mN8pQwR5vX2yZ4b")
	if a != b {
		t.Errorf("checksum not deterministic: %q vs %q", a, b)
	}
}
