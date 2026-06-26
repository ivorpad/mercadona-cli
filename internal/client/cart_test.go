package client

import (
	"encoding/json"
	"strings"
	"testing"
)

func qtyOf(lines []CartLine, id string) (float64, bool) {
	for _, l := range lines {
		if l.ProductID == id {
			return l.Quantity, true
		}
	}
	return 0, false
}

func TestApplyLine(t *testing.T) {
	base := []CartLine{
		{ProductID: "1", Quantity: 2},
		{ProductID: "2", Quantity: 1},
	}

	t.Run("absolute set on existing", func(t *testing.T) {
		got := ApplyLine(base, "1", 5, false)
		if q, _ := qtyOf(got, "1"); q != 5 {
			t.Fatalf("got %v, want 5", q)
		}
	})
	t.Run("additive on existing", func(t *testing.T) {
		got := ApplyLine(base, "2", 3, true)
		if q, _ := qtyOf(got, "2"); q != 4 {
			t.Fatalf("got %v, want 4", q)
		}
	})
	t.Run("set 0 removes the line", func(t *testing.T) {
		got := ApplyLine(base, "1", 0, false)
		if _, ok := qtyOf(got, "1"); ok {
			t.Fatalf("line 1 should have been removed: %+v", got)
		}
		if len(got) != 1 {
			t.Fatalf("want 1 line left, got %d", len(got))
		}
	})
	t.Run("new line via add", func(t *testing.T) {
		got := ApplyLine(base, "9", 2, true)
		if q, _ := qtyOf(got, "9"); q != 2 {
			t.Fatalf("got %v, want 2", q)
		}
		if len(got) != 3 {
			t.Fatalf("want 3 lines, got %d", len(got))
		}
	})
	t.Run("set 0 on missing id is a no-op", func(t *testing.T) {
		got := ApplyLine(base, "404", 0, false)
		if len(got) != 2 {
			t.Fatalf("want 2 lines unchanged, got %d", len(got))
		}
	})
	t.Run("does not mutate input", func(t *testing.T) {
		_ = ApplyLine(base, "1", 99, false)
		if q, _ := qtyOf(base, "1"); q != 2 {
			t.Fatalf("input mutated: line 1 = %v", q)
		}
	})
}

func TestCartLineUnmarshalGET(t *testing.T) {
	raw := `{"quantity":2,"sources":[],"product":{"id":"60393","display_name":"Gambón","price_instructions":{"unit_price":"6.00","reference_price":"12.000","reference_format":"kg"}}}`
	var l CartLine
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		t.Fatal(err)
	}
	if l.ProductID != "60393" || l.Quantity != 2 {
		t.Fatalf("id/qty wrong: %+v", l)
	}
	if l.DisplayName != "Gambón" || l.UnitPrice != "6.00" || l.RefPrice != "12.000" || l.RefFormat != "kg" {
		t.Fatalf("name/price not captured: %+v", l)
	}
}

func TestCartLineMarshalPUTStaysFlat(t *testing.T) {
	// GET-only fields (display_name, unit_price) must never be sent on a PUT.
	l := CartLine{ProductID: "60393", Quantity: 2, DisplayName: "ignored", UnitPrice: "6.00"}
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"product_id":"60393"`) || !strings.Contains(s, `"quantity":2`) {
		t.Fatalf("PUT shape missing flat fields: %s", s)
	}
	if strings.Contains(s, "display_name") || strings.Contains(s, "unit_price") || strings.Contains(s, "product\":") {
		t.Fatalf("PUT shape leaked GET-only fields: %s", s)
	}
}
