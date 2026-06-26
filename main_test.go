package main

import (
	"flag"
	"reflect"
	"testing"
)

// checkBudget is the safety-critical gate — exercise the table exhaustively.
func TestCheckBudget(t *testing.T) {
	cases := []struct {
		name       string
		total      float64
		haveTotal  bool
		maxEUR     float64
		failClosed bool
		wantErr    bool
	}{
		{"under the cap", 76.84, true, 100, false, false},
		{"exactly at the cap is allowed", 100, true, 100, false, false},
		{"over the cap fails", 150, true, 100, false, true},
		{"over the cap fails (failClosed)", 150, true, 100, true, true},
		{"no cap configured disables the guard", 999999, true, 0, true, false},
		{"unknown total is allowed when soft", 0, false, 100, false, false},
		{"unknown total refuses when failClosed (submit)", 0, false, 100, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkBudget(c.total, c.haveTotal, c.maxEUR, "test", c.failClosed)
			if (err != nil) != c.wantErr {
				t.Errorf("checkBudget(total=%v have=%v max=%v failClosed=%v) err=%v, wantErr=%v",
					c.total, c.haveTotal, c.maxEUR, c.failClosed, err, c.wantErr)
			}
		})
	}
}

// reorderArgs must let flags appear after positionals (otherwise --max would be
// silently dropped — a safety footgun).
func TestReorderArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		max  float64
		json bool
		pos  []string
	}{
		{"flag after positionals", []string{"add", "123", "5", "--max", "50"}, 50, false, []string{"add", "123", "5"}},
		{"bool flag interspersed", []string{"add", "--json", "123"}, 0, true, []string{"add", "123"}},
		{"equals form after positional", []string{"add", "123", "--max=20"}, 20, false, []string{"add", "123"}},
		{"flags first still work", []string{"--max", "9", "--json", "x"}, 9, true, []string{"x"}},
		{"double-dash terminator", []string{"--max", "9", "--", "--weird"}, 9, false, []string{"--weird"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := flag.NewFlagSet("t", flag.ContinueOnError)
			maxV := fs.Float64("max", 0, "")
			jsonV := fs.Bool("json", false, "")
			fs.String("wh", "mad1", "")
			if err := fs.Parse(reorderArgs(fs, c.in)); err != nil {
				t.Fatalf("parse(%v): %v", c.in, err)
			}
			if *maxV != c.max || *jsonV != c.json || !reflect.DeepEqual(fs.Args(), c.pos) {
				t.Errorf("in=%v → max=%v json=%v pos=%v; want max=%v json=%v pos=%v",
					c.in, *maxV, *jsonV, fs.Args(), c.max, c.json, c.pos)
			}
		})
	}
}
