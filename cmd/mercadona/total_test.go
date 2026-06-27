package main

import (
	"math"
	"testing"
)

// Money parsing/formatting is the trust-critical core of `total` — exercise it
// directly so a refactor can't silently drift a euro figure.
func TestPriceCents(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"1.20", 120, false},
		{"0.95", 95, false},
		{"5.85", 585, false},
		{"38.60", 3860, false},
		{" 3.20 ", 320, false}, // trimmed
		{"1.875", 188, false},  // rounds half away from zero
		{"0", 0, false},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := priceCents(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("priceCents(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("priceCents(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestCentsStr(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{120, "1.20"},
		{3860, "38.60"},
		{5, "0.05"},
		{0, "0.00"},
		{100, "1.00"},
		{293, "2.93"},
	}
	for _, c := range cases {
		if got := centsStr(c.in); got != c.want {
			t.Errorf("centsStr(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// fractional quantities (weight/bulk items) must round to a cent deterministically.
func TestSubtotalRounding(t *testing.T) {
	cents, err := priceCents("5.85")
	if err != nil {
		t.Fatal(err)
	}
	sub := int64(math.Round(float64(cents) * 0.5)) // 292.5 → 293
	if got := centsStr(sub); got != "2.93" {
		t.Errorf("5.85 × 0.5 = %s, want 2.93", got)
	}
}

func TestParseBasketLine(t *testing.T) {
	ok := func(s, id string, qty float64) {
		t.Helper()
		bl, err := parseBasketLine(s)
		if err != nil {
			t.Errorf("parseBasketLine(%q) unexpected err: %v", s, err)
			return
		}
		if bl.id != id || bl.qty != qty {
			t.Errorf("parseBasketLine(%q) = {%s, %v}, want {%s, %v}", s, bl.id, bl.qty, id, qty)
		}
	}
	bad := func(s string) {
		t.Helper()
		if _, err := parseBasketLine(s); err == nil {
			t.Errorf("parseBasketLine(%q) expected error", s)
		}
	}
	ok("5044", "5044", 1)
	ok("2779 2", "2779", 2)
	ok("87177 0.5", "87177", 0.5)
	bad("x 0")   // non-positive qty
	bad("x -1")  // negative qty
	bad("x abc") // unparseable qty
	bad("a b c") // too many fields
}

func TestStripComment(t *testing.T) {
	cases := map[string]string{
		"5044 1  # Arroz redondo Hacendado": "5044 1", // trailing comment + name
		"5044 1#no space":                   "5044 1", // no space before #
		"# whole-line comment":              "",       // whole-line
		"   # indented comment":             "",
		"60393 2":                           "60393 2", // unchanged
		"  82830.1 1   ":                    "82830.1 1",
	}
	for in, want := range cases {
		if got := stripComment(in); got != want {
			t.Errorf("stripComment(%q) = %q, want %q", in, got, want)
		}
	}
}
