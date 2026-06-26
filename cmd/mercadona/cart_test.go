package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ivorjpc/mercadona/internal/client"
)

func TestCollectSetManyPositional(t *testing.T) {
	ch, err := collectSetMany("", []string{"10", "2", "20", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ch) != 2 {
		t.Fatalf("want 2 changes, got %d", len(ch))
	}
	if ch[0].id != "10" || ch[0].qty != 2 || ch[0].add {
		t.Fatalf("change 0 wrong: %+v", ch[0])
	}
	if ch[1].id != "20" || ch[1].qty != 0 { // qty 0 allowed (= remove)
		t.Fatalf("change 1 wrong (qty 0 must be allowed): %+v", ch[1])
	}
}

func TestCollectSetManyErrors(t *testing.T) {
	cases := map[string][]string{
		"odd token count": {"10"},
		"non-numeric qty": {"10", "x"},
		"negative qty":    {"10", "-1"},
	}
	for name, args := range cases {
		if _, err := collectSetMany("", args); err == nil {
			t.Errorf("%s: expected an error, got nil", name)
		}
	}
}

func TestReadChangesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "changes.txt")
	if err := os.WriteFile(p, []byte("# a comment\n10 2\n20 0\n\n30 1.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch, err := readChangesFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []cartChange{{id: "10", qty: 2}, {id: "20", qty: 0}, {id: "30", qty: 1.5}}
	if len(ch) != len(want) {
		t.Fatalf("want %d changes, got %d (%+v)", len(want), len(ch), ch)
	}
	for i, w := range want {
		if ch[i] != w {
			t.Errorf("change %d = %+v, want %+v", i, ch[i], w)
		}
	}
}

// priceBasket reuses prices already present on the seed lines (from a cart GET),
// so a fully-seeded basket needs no network — exercise that path here.
func TestPriceBasketSeeded(t *testing.T) {
	seed := []client.CartLine{
		{ProductID: "1", UnitPrice: "1.20", DisplayName: "Arroz"},
		{ProductID: "2", UnitPrice: "6.00", DisplayName: "Gambón"},
	}
	lines := []client.CartLine{
		{ProductID: "1", Quantity: 2}, // 2 × 1.20 = 2.40
		{ProductID: "2", Quantity: 1}, // 1 × 6.00 = 6.00
	}
	bp := priceBasket(client.New(), lines, seed)
	if !bp.complete {
		t.Fatalf("basket should be complete from the seed (no fetch needed)")
	}
	if bp.totalCents != 840 {
		t.Fatalf("totalCents = %d, want 840", bp.totalCents)
	}
	if bp.names["2"] != "Gambón" {
		t.Fatalf("name not carried: %q", bp.names["2"])
	}
}

func TestPriceBasketFractionalQty(t *testing.T) {
	seed := []client.CartLine{{ProductID: "9", UnitPrice: "10.00", DisplayName: "Jamón"}}
	lines := []client.CartLine{{ProductID: "9", Quantity: 0.35}} // 0.35 × 10.00 = 3.50
	bp := priceBasket(client.New(), lines, seed)
	if !bp.complete || bp.totalCents != 350 {
		t.Fatalf("got complete=%v cents=%d, want true/350", bp.complete, bp.totalCents)
	}
}
