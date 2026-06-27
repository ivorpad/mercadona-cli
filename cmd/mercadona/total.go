package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

// total computes a deterministic basket total from '<id> [qty]' lines: the CLI
// fetches each product's current price and sums unit_price×qty in integer cents,
// so the figure is exact and reproducible — not hand-added by a caller. This is
// the pre-cart estimate; the authoritative total still comes from the cart/checkout
// API. Quantities may be fractional (weight/bulk items). No login needed (prices
// are public); ids are per-warehouse, so it prices in the configured/--wh warehouse.
func cmdTotal(args []string) error {
	fs := flag.NewFlagSet("total", flag.ExitOnError)
	cf := addCommon(fs)
	file := fs.String("f", "", "file with one '<id> [qty]' per line ('-' for stdin); else ids are positional args (qty 1)")
	_ = fs.Parse(reorderArgs(fs, args))

	lines, err := collectBasket(*file, fs.Args())
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("no basket lines (use -f file, stdin, or '<id> [<id>...]' args)")
	}
	cl := newClient(cf)

	type lineResult struct {
		ID        string  `json:"id"`
		Name      string  `json:"name,omitempty"`
		Qty       float64 `json:"qty"`
		UnitPrice string  `json:"unit_price,omitempty"`
		Subtotal  string  `json:"subtotal,omitempty"`
		Error     string  `json:"error,omitempty"`
	}
	results := make([]lineResult, 0, len(lines))
	var totalCents int64
	failed := 0
	for _, bl := range lines {
		pv, _, perr := cl.Product(bl.id)
		if perr != nil {
			results = append(results, lineResult{ID: bl.id, Qty: bl.qty, Error: perr.Error()})
			failed++
			continue
		}
		cents, cerr := priceCents(pv.Price.UnitPrice)
		if cerr != nil {
			results = append(results, lineResult{ID: bl.id, Name: pv.DisplayName, Qty: bl.qty, UnitPrice: pv.Price.UnitPrice, Error: cerr.Error()})
			failed++
			continue
		}
		sub := int64(math.Round(float64(cents) * bl.qty))
		totalCents += sub
		results = append(results, lineResult{
			ID: bl.id, Name: pv.DisplayName, Qty: bl.qty,
			UnitPrice: pv.Price.UnitPrice, Subtotal: centsStr(sub),
		})
	}

	if cf.jsonOut {
		if emitErr := emitJSON(map[string]any{
			"lines":    results,
			"total":    centsStr(totalCents),
			"count":    len(lines),
			"complete": failed == 0,
		}); emitErr != nil {
			return emitErr
		}
	} else {
		for _, r := range results {
			if r.Error != "" {
				fmt.Printf("  [%s] %s  ERROR: %s\n", r.ID, orDefault(r.Name, "?"), r.Error)
				continue
			}
			fmt.Printf("  [%s] %s — %s × %s€ = %s€\n", r.ID, r.Name, fmtQty(r.Qty), r.UnitPrice, r.Subtotal)
		}
		fmt.Printf("  total: %s€  (%d líneas)\n", centsStr(totalCents), len(lines))
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d lines could not be priced (excluded from the total) — check the ids exist in the %s warehouse", failed, len(lines), cl.Warehouse)
	}
	return nil
}

type basketLine struct {
	id  string
	qty float64
}

// collectBasket reads basket lines from a file/stdin (-f, one '<id> [qty]' per
// line, '#' comments skipped) or, with no file, from positional args (each a bare
// id with qty 1). A per-line missing qty defaults to 1.
func collectBasket(file string, posArgs []string) ([]basketLine, error) {
	if file == "" {
		lines := make([]basketLine, 0, len(posArgs))
		for _, a := range posArgs {
			lines = append(lines, basketLine{id: a, qty: 1})
		}
		return lines, nil
	}
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
	var lines []basketLine
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	ln := 0
	for sc.Scan() {
		ln++
		t := stripComment(sc.Text())
		if t == "" {
			continue
		}
		bl, err := parseBasketLine(t)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", ln, err)
		}
		lines = append(lines, bl)
	}
	return lines, sc.Err()
}

// stripComment drops a '#' comment — whole-line or trailing — and trims, so basket
// and set-many files can annotate ids with the product name ("5044 1  # Arroz").
// Product ids are numeric, so '#' is never part of one.
func stripComment(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func parseBasketLine(s string) (basketLine, error) {
	f := strings.Fields(s)
	switch len(f) {
	case 1:
		return basketLine{id: f[0], qty: 1}, nil
	case 2:
		q, err := strconv.ParseFloat(f[1], 64)
		if err != nil || q <= 0 {
			return basketLine{}, fmt.Errorf("invalid qty %q (want a positive number)", f[1])
		}
		return basketLine{id: f[0], qty: q}, nil
	default:
		return basketLine{}, fmt.Errorf("expected '<id> [qty]', got %q", s)
	}
}

// priceCents parses a "1.20"-style euro price into integer cents, so totals sum
// exactly with no floating-point drift across many lines.
func priceCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty price")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("unparseable price %q", s)
	}
	return int64(math.Round(v * 100)), nil
}

// centsStr renders integer cents as a "35.00"-style euro string.
func centsStr(c int64) string {
	return fmt.Sprintf("%d.%02d", c/100, c%100)
}
