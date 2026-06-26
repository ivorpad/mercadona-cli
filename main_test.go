package main

import "testing"

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
