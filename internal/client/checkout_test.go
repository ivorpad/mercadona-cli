package client

import "testing"

func TestExtractTotal(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want float64
		ok   bool
	}{
		{"cart summary string", `{"summary":{"total":"76.84"}}`, 76.84, true},
		{"checkout price number", `{"price":{"total":129.5}}`, 129.5, true},
		{"top-level total string", `{"total":"12.30"}`, 12.30, true},
		{"summary preferred over price", `{"summary":{"total":"10.00"},"price":{"total":"99.00"}}`, 10.00, true},
		{"missing", `{"id":"abc","lines":[]}`, 0, false},
		{"zero ignored", `{"summary":{"total":"0"}}`, 0, false},
		{"garbage tolerated", `{"summary":{"total":"n/a"}}`, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ExtractTotal([]byte(c.raw))
			if ok != c.ok || (ok && got != c.want) {
				t.Errorf("ExtractTotal(%s) = %v,%v; want %v,%v", c.raw, got, ok, c.want, c.ok)
			}
		})
	}
}
