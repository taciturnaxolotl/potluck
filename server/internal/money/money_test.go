package money

import "testing"

func TestParseUSD(t *testing.T) {
	cases := []struct {
		in   string
		want Micros
	}{
		{"0", 0},
		{"1", FromUSD(1)},
		{"1.23", FromCents(123)},
		{"0.000125", 125},
		{"-0.50", -FromCents(50)},
		{"+2.5", FromCents(250)},
		{".25", FromCents(25)},
		{"1.2345678", FromCents(123) + 4567}, // truncates to 6 digits
	}
	for _, c := range cases {
		got, err := ParseUSD(c.in)
		if err != nil {
			t.Fatalf("ParseUSD(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseUSD(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseUSDErrors(t *testing.T) {
	for _, in := range []string{"", "abc", "1.2.3", "--1"} {
		if _, err := ParseUSD(in); err == nil {
			t.Errorf("ParseUSD(%q) expected error", in)
		}
	}
}

func TestUSDString(t *testing.T) {
	cases := []struct {
		in   Micros
		want string
	}{
		{0, "0.00"},
		{FromUSD(1), "1.00"},
		{FromCents(123), "1.23"},
		{125, "0.000125"},
		{-FromCents(50), "-0.50"},
		{FromCents(123) + 4567, "1.234567"},
	}
	for _, c := range cases {
		if got := c.in.USDString(); got != c.want {
			t.Errorf("Micros(%d).USDString() = %q, want %q", c.in, got, c.want)
		}
	}
}
