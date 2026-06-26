package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/ivorjpc/mercadona/internal/client"
)

// MinOrderEUR is the observed Spain-wide minimum basket for home delivery. It is
// advisory only — surfaced as a "faltan X€" hint, never enforced — since the
// authoritative minimum is whatever the checkout API accepts and can vary by
// warehouse. Kept here so `cart get`/`set-many` can nudge before checkout fails.
const MinOrderEUR = 60.0

// fmtQty renders a quantity without a trailing ".0" (1.0 → "1", 0.5 → "0.5").
func fmtQty(q float64) string {
	return strconv.FormatFloat(q, 'f', -1, 64)
}

func cmdCart(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mercadona cart <get|add|set|set-many|clear> [flags] [args]")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("cart", flag.ExitOnError)
	cf := addCommon(fs)
	maxFlag := fs.Float64("max", 0, "refuse if the resulting cart total exceeds this many € (0 = env/config)")
	file := fs.String("f", "", "set-many: file with one '<id> <qty>' per line ('-' for stdin; 0 removes)")
	_ = fs.Parse(reorderArgs(fs, rest))
	cl, err := authedClient(cf)
	if err != nil {
		return err
	}
	switch sub {
	case "get":
		return cartGet(cl, cf)
	case "clear":
		return cartClear(cl, cf)
	case "add", "set":
		a := fs.Args()
		if len(a) != 2 {
			return fmt.Errorf("usage: mercadona cart %s <product_id> <qty>", sub)
		}
		qty, perr := strconv.ParseFloat(a[1], 64)
		if perr != nil {
			return fmt.Errorf("invalid qty %q", a[1])
		}
		return cartApply(cl, cf, []cartChange{{id: a[0], qty: qty, add: sub == "add"}}, *maxFlag, sub)
	case "set-many":
		changes, cerr := collectSetMany(*file, fs.Args())
		if cerr != nil {
			return cerr
		}
		if len(changes) == 0 {
			return fmt.Errorf("no changes (use -f file/stdin with '<id> <qty>' lines, or '<id> <qty> ...' args)")
		}
		return cartApply(cl, cf, changes, *maxFlag, "set-many")
	default:
		return fmt.Errorf("unknown cart subcommand %q (get|add|set|set-many|clear)", sub)
	}
}

// ---- read ----

func cartGet(cl *client.Client, cf *common) error {
	cart, raw, err := cl.GetCart()
	if err != nil {
		return err
	}
	if cf.jsonOut {
		return emitRaw(raw)
	}
	fmt.Printf("cart %s  (v%d, %d productos, total %s€)\n", cart.ID, cart.Version, cart.ProductsCount, cart.Summary.Total)
	for _, l := range cart.Lines {
		printCartLine(l)
	}
	printMinOrderHint(cart.Summary.Total)
	return nil
}

// printCartLine renders one line as 'name — qty × unit_price = subtotal' when the
// GET carried a price, falling back to a bare 'qty × name' otherwise.
func printCartLine(l client.CartLine) {
	name := orDefault(l.DisplayName, "product "+l.ProductID)
	if l.UnitPrice == "" {
		fmt.Printf("  [%s] %s × %s\n", l.ProductID, fmtQty(l.Quantity), name)
		return
	}
	sub := "?"
	if cents, err := priceCents(l.UnitPrice); err == nil {
		sub = centsStr(int64(math.Round(float64(cents)*l.Quantity))) + "€"
	}
	fmt.Printf("  [%s] %s — %s × %s€ = %s\n", l.ProductID, name, fmtQty(l.Quantity), l.UnitPrice, sub)
}

// printMinOrderHint warns when the basket is below the delivery minimum.
func printMinOrderHint(totalStr string) {
	t, err := strconv.ParseFloat(strings.TrimSpace(totalStr), 64)
	if err != nil || t <= 0 || t >= MinOrderEUR {
		return
	}
	fmt.Printf("  ⚠ faltan %.2f€ para el mínimo de pedido de %.0f€\n", MinOrderEUR-t, MinOrderEUR)
}

// ---- write ----

func cartClear(cl *client.Client, cf *common) error {
	cart, _, err := cl.GetCart()
	if err != nil {
		return err
	}
	n := len(cart.Lines)
	if n == 0 {
		fmt.Println("✓ el carrito ya está vacío")
		return nil
	}
	cart.Lines = []client.CartLine{}
	raw, err := cl.PutCart(cart)
	if err != nil {
		return err
	}
	if cf.jsonOut {
		return emitRaw(raw)
	}
	fmt.Printf("✓ carrito vaciado (%d productos eliminados)\n", n)
	return nil
}

// cartChange is one desired mutation: an additive bump (add=true) or an absolute
// quantity set (add=false; qty 0 removes the line).
type cartChange struct {
	id  string
	qty float64
	add bool
}

// cartApply is the shared GET → apply-locally → price → (preventive) budget guard
// → single PUT flow behind add/set/set-many. Pricing the resulting basket locally
// (seeded from the prices already in the GET, so only new ids are fetched) lets
// --max refuse BEFORE the write — and a final authoritative check reverts the cart
// if the API total still breaches the cap (promos/rounding the estimate missed).
func cartApply(cl *client.Client, cf *common, changes []cartChange, maxFlag float64, action string) error {
	cart, _, err := cl.GetCart()
	if err != nil {
		return err
	}
	prev := cart.Lines
	lines := cart.Lines
	for _, ch := range changes {
		lines = client.ApplyLine(lines, ch.id, ch.qty, ch.add)
	}

	maxEUR, err := resolveMax(maxFlag)
	if err != nil {
		return err
	}

	var est *basketPrice
	if maxEUR > 0 {
		est = priceBasket(cl, lines, prev)
		if est.complete && est.eur() > maxEUR {
			return fmt.Errorf("BUDGET EXCEEDED (estimated ≈%.2f€ > %.2f€ limit): not writing %s — raise the cap with --max, MERCADONA_MAX_EUR, or [limits].max_eur", est.eur(), maxEUR, action)
		}
	}

	cart.Lines = lines
	raw, err := cl.PutCart(cart)
	if err != nil {
		return err
	}

	// Authoritative backstop: if the real total breaches the cap, restore the
	// pre-change basket so we never leave the cart over budget.
	if maxEUR > 0 {
		if total, ok := client.ExtractTotal(raw); ok && total > maxEUR {
			_, _ = cl.PutCart(&client.Cart{ID: cart.ID, Lines: prev})
			return fmt.Errorf("BUDGET EXCEEDED: %s landed at %.2f€, over the %.2f€ limit — reverted the cart (raise --max to proceed)", action, total, maxEUR)
		}
	}

	if cf.jsonOut {
		return emitRaw(raw)
	}
	printApplyResult(raw, lines, est, changes, action)
	return nil
}

// basketPrice is a local estimate of a basket's total (summed in integer cents),
// plus the display names gathered while pricing. complete is false when any line
// couldn't be priced (then the estimate is a lower bound and the guard defers to
// the authoritative post-write total).
type basketPrice struct {
	totalCents int64
	complete   bool
	names      map[string]string
}

func (b *basketPrice) eur() float64 { return float64(b.totalCents) / 100 }

// priceFetchConcurrency bounds the concurrent product-price GETs when set-many
// prices a fresh basket. Kept deliberately low to stay web-app-paced and avoid
// tripping rate limits; override with MERCADONA_CONCURRENCY (1–16). The HTTP client
// also backs off on 429/503, so this only governs how hard we push before that.
func priceFetchConcurrency() int {
	if s := os.Getenv("MERCADONA_CONCURRENCY"); s != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 1 && n <= 16 {
			return n
		}
	}
	return 4
}

// priceBasket sums unit_price×qty over lines. Prices already present on the seed
// lines (from the cart GET) are reused; the rest are fetched concurrently.
func priceBasket(cl *client.Client, lines, seed []client.CartLine) *basketPrice {
	type entry struct {
		cents int64
		name  string
		ok    bool
	}
	cache := map[string]entry{}
	for _, s := range seed {
		if s.UnitPrice == "" {
			continue
		}
		if c, err := priceCents(s.UnitPrice); err == nil {
			cache[s.ProductID] = entry{cents: c, name: s.DisplayName, ok: true}
		}
	}

	// Ids still needing a price fetch (deduped, order-stable).
	var need []string
	seen := map[string]bool{}
	for _, l := range lines {
		if _, ok := cache[l.ProductID]; ok || seen[l.ProductID] {
			continue
		}
		seen[l.ProductID] = true
		need = append(need, l.ProductID)
	}

	if len(need) > 0 {
		fetched := make([]entry, len(need))
		var wg sync.WaitGroup
		sem := make(chan struct{}, priceFetchConcurrency()) // bound concurrent product GETs
		for i, id := range need {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, id string) {
				defer wg.Done()
				defer func() { <-sem }()
				pv, _, err := cl.Product(id)
				if err != nil {
					return
				}
				c, cerr := priceCents(pv.Price.UnitPrice)
				if cerr != nil {
					return
				}
				fetched[i] = entry{cents: c, name: pv.DisplayName, ok: true}
			}(i, id)
		}
		wg.Wait()
		for i, id := range need {
			if fetched[i].ok {
				cache[id] = fetched[i]
			}
		}
	}

	bp := &basketPrice{complete: true, names: map[string]string{}}
	for _, l := range lines {
		e, ok := cache[l.ProductID]
		if !ok || !e.ok {
			bp.complete = false
			continue
		}
		bp.names[l.ProductID] = e.name
		bp.totalCents += int64(math.Round(float64(e.cents) * l.Quantity))
	}
	return bp
}

// printApplyResult prints the concise human summary after a write: the new cart
// total/count plus the touched line(s). It prefers the authoritative PUT response
// and falls back to the local estimate when the response omits those fields.
func printApplyResult(raw json.RawMessage, localLines []client.CartLine, est *basketPrice, changes []cartChange, action string) {
	var updated client.Cart
	_ = json.Unmarshal(raw, &updated)

	count := updated.ProductsCount
	if count == 0 {
		count = len(localLines)
	}
	totalStr := "?"
	if updated.Summary.Total != "" {
		totalStr = updated.Summary.Total + "€"
	} else if est != nil && est.complete {
		totalStr = "≈" + centsStr(est.totalCents) + "€"
	}

	qtyOf := func(id string) (float64, bool) {
		for _, l := range localLines {
			if l.ProductID == id {
				return l.Quantity, true
			}
		}
		return 0, false
	}
	nameOf := func(id string) string {
		for _, l := range updated.Lines {
			if l.ProductID == id && l.DisplayName != "" {
				return l.DisplayName
			}
		}
		if est != nil {
			if n := est.names[id]; n != "" {
				return n
			}
		}
		return "product " + id
	}
	lineFor := func(id string) string {
		if q, ok := qtyOf(id); ok {
			return fmt.Sprintf("[%s] %s → x%s", id, nameOf(id), fmtQty(q))
		}
		return fmt.Sprintf("[%s] %s → (eliminado)", id, nameOf(id))
	}

	if action == "set-many" {
		fmt.Printf("✓ set-many: %d cambios  |  carrito: %d productos, total %s\n", len(changes), count, totalStr)
		for _, ch := range changes {
			fmt.Printf("    %s\n", lineFor(ch.id))
		}
	} else {
		fmt.Printf("✓ %s %s  |  carrito: %d productos, total %s\n", action, lineFor(changes[0].id), count, totalStr)
	}
	printMinOrderHint(updated.Summary.Total)
}

// ---- set-many input parsing ----

// collectSetMany reads desired '<id> <qty>' changes from a file/stdin (-f) or,
// with no file, from positional '<id> <qty>' pairs. Unlike the `total` basket
// reader, qty 0 is allowed and means "remove that line".
func collectSetMany(file string, posArgs []string) ([]cartChange, error) {
	if file != "" {
		return readChangesFile(file)
	}
	if len(posArgs)%2 != 0 {
		return nil, fmt.Errorf("set-many positional args must be '<id> <qty>' pairs (got %d tokens) — or use -f file", len(posArgs))
	}
	changes := make([]cartChange, 0, len(posArgs)/2)
	for i := 0; i < len(posArgs); i += 2 {
		q, err := strconv.ParseFloat(posArgs[i+1], 64)
		if err != nil || q < 0 {
			return nil, fmt.Errorf("invalid qty %q for id %q (want a number ≥ 0; 0 removes)", posArgs[i+1], posArgs[i])
		}
		changes = append(changes, cartChange{id: posArgs[i], qty: q})
	}
	return changes, nil
}

func readChangesFile(file string) ([]cartChange, error) {
	var r io.Reader
	if file == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	var changes []cartChange
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	ln := 0
	for sc.Scan() {
		ln++
		t := strings.TrimSpace(sc.Text())
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		f := strings.Fields(t)
		if len(f) != 2 {
			return nil, fmt.Errorf("line %d: expected '<id> <qty>', got %q", ln, t)
		}
		q, err := strconv.ParseFloat(f[1], 64)
		if err != nil || q < 0 {
			return nil, fmt.Errorf("line %d: invalid qty %q (want a number ≥ 0; 0 removes)", ln, f[1])
		}
		changes = append(changes, cartChange{id: f[0], qty: q})
	}
	return changes, sc.Err()
}
