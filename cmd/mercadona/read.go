package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ivorjpc/mercadona/internal/client"
)

// Top-level category ids excluded by --fresh: frozen and canned/preserved goods,
// which otherwise dominate the top hits for "gambas", "mejillón", "atún", etc.
const (
	catCongelados = 17 // Congelados (frozen)
	catConservas  = 14 // Conservas, caldos y cremas (canned / preserved)
)

// categoryFilters turns the --category (id or name) and --fresh flags into Algolia
// facetFilters, AND-ed together. Empty when neither is set.
func categoryFilters(cl *client.Client, category string, fresh bool) ([]string, error) {
	var filters []string
	if fresh {
		filters = append(filters,
			fmt.Sprintf("categories.id:-%d", catCongelados),
			fmt.Sprintf("categories.id:-%d", catConservas),
		)
	}
	if category != "" {
		id, err := resolveCategoryID(cl, category)
		if err != nil {
			return nil, err
		}
		filters = append(filters, fmt.Sprintf("categories.id:%d", id))
	}
	return filters, nil
}

// resolveCategoryID returns a numeric category id directly, or resolves a name
// against the warehouse category tree (case-insensitive; exact match wins over a
// substring, ambiguity is an error that lists the candidates).
func resolveCategoryID(cl *client.Client, arg string) (int, error) {
	if n, err := strconv.Atoi(strings.TrimSpace(arg)); err == nil {
		return n, nil
	}
	raw, err := cl.Categories()
	if err != nil {
		return 0, fmt.Errorf("resolve category %q: %w", arg, err)
	}
	var tree struct {
		Results []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Categories []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"categories"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return 0, fmt.Errorf("parse categories: %w", err)
	}
	type cat struct {
		id   int
		name string
	}
	var all []cat
	for _, t := range tree.Results {
		all = append(all, cat{t.ID, t.Name})
		for _, s := range t.Categories {
			all = append(all, cat{s.ID, s.Name})
		}
	}
	want := strings.ToLower(strings.TrimSpace(arg))
	var exact, partial []cat
	for _, c := range all {
		lc := strings.ToLower(c.name)
		switch {
		case lc == want:
			exact = append(exact, c)
		case strings.Contains(lc, want):
			partial = append(partial, c)
		}
	}
	pick := exact
	if len(pick) == 0 {
		pick = partial
	}
	switch len(pick) {
	case 0:
		return 0, fmt.Errorf("no category matches %q — run `mercadona categories` to list ids", arg)
	case 1:
		return pick[0].id, nil
	default:
		names := make([]string, len(pick))
		for i, c := range pick {
			names[i] = fmt.Sprintf("%d %s", c.id, c.name)
		}
		return 0, fmt.Errorf("category %q is ambiguous (%s) — pass a numeric id", arg, strings.Join(names, ", "))
	}
}

func cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	cf := addCommon(fs)
	limit := fs.Int("limit", 24, "max results")
	category := fs.String("category", "", "filter to a category by id or name (see `mercadona categories`)")
	fresh := fs.Bool("fresh", false, "exclude frozen (Congelados) and canned (Conservas) results")
	_ = fs.Parse(reorderArgs(fs, args))
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: mercadona search [flags] <term...>")
	}
	cl := newClient(cf)
	filters, err := categoryFilters(cl, *category, *fresh)
	if err != nil {
		return err
	}
	res, err := cl.Search(strings.Join(fs.Args(), " "), *limit, filters...)
	if err != nil {
		return err
	}
	if cf.jsonOut {
		return emitJSON(res)
	}
	fmt.Printf("query=%q  nbHits=%d  (index=%s)\n", res.Query, res.NbHits, cl.IndexName())
	for _, h := range res.Hits {
		printHit("  ", h)
	}
	return nil
}

func cmdBatch(args []string) error {
	fs := flag.NewFlagSet("batch", flag.ExitOnError)
	cf := addCommon(fs)
	file := fs.String("f", "", "file with one term per line ('-' for stdin); else terms are positional args")
	hits := fs.Int("hits", 1, "results per term")
	category := fs.String("category", "", "filter every term to a category by id or name (see `mercadona categories`)")
	fresh := fs.Bool("fresh", false, "exclude frozen (Congelados) and canned (Conservas) results")
	_ = fs.Parse(reorderArgs(fs, args))
	terms, err := collectTerms(*file, fs.Args())
	if err != nil {
		return err
	}
	if len(terms) == 0 {
		return fmt.Errorf("no terms given (use -f file, stdin, or positional args)")
	}
	cl := newClient(cf)
	filters, err := categoryFilters(cl, *category, *fresh)
	if err != nil {
		return err
	}
	results, err := cl.Batch(terms, *hits, filters...)
	if err != nil {
		return err
	}
	if cf.jsonOut {
		return emitJSON(results)
	}
	for i, r := range results {
		term := r.Query
		if term == "" && i < len(terms) {
			term = terms[i]
		}
		if len(r.Hits) == 0 {
			fmt.Printf("• %-24s → (sin resultados)\n", term)
			continue
		}
		h := r.Hits[0]
		fmt.Printf("• %-24s → [%s] %s — %s€ %s\n", term, h.ID, h.DisplayName, h.Price.UnitPrice, refFormat(h.Price))
	}
	return nil
}

func cmdProduct(args []string) error {
	fs := flag.NewFlagSet("product", flag.ExitOnError)
	cf := addCommon(fs)
	_ = fs.Parse(reorderArgs(fs, args))
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: mercadona product [flags] <id>")
	}
	cl := newClient(cf)
	pv, raw, err := cl.Product(fs.Arg(0))
	if err != nil {
		return err
	}
	if cf.jsonOut {
		return emitRaw(raw)
	}
	fmt.Printf("[%s] %s\n", pv.ID, pv.DisplayName)
	fmt.Printf("  precio: %s€  (%s %s)\n", pv.Price.UnitPrice, pv.Price.ReferencePrice, pv.Price.ReferenceFormat)
	if pv.Packaging != "" {
		fmt.Printf("  formato: %s\n", pv.Packaging)
	}
	if pv.ShareURL != "" {
		fmt.Printf("  url: %s\n", pv.ShareURL)
	}
	for _, line := range nutritionLines(pv.Nutrition()) {
		fmt.Println(line)
	}
	return nil
}

// nutritionLines renders a product's nutrition table as indented human lines,
// or nil when the product carries none (most staples). Amounts arrive as decimal
// strings like "385.0"; a whole-number ".0" is dropped so it reads as the label
// does ("385 kcal"), while "9.2" is left intact.
func nutritionLines(n *client.Nutrition) []string {
	if n == nil {
		return nil
	}
	head := "  nutrición"
	if n.PerQuantity != "" {
		head += " (" + n.PerQuantity + ")"
	}
	lines := []string{head + ":"}
	if n.EnergyCalories.Amount != "" || n.EnergyJoules.Amount != "" {
		lines = append(lines, fmt.Sprintf("    energía: %s / %s",
			amountUnit(n.EnergyCalories), amountUnit(n.EnergyJoules)))
	}
	for _, nut := range n.Nutrients {
		lines = append(lines, "    "+nutrientStr(nut))
		if nut.SubNutrients != nil {
			for _, it := range nut.SubNutrients.Items {
				lines = append(lines, "      "+nutrientStr(it))
			}
		}
	}
	return lines
}

// nutrientStr formats one labelled row as "Name: amount unit" (e.g. "Grasas: 29 g").
func nutrientStr(n client.Nutrient) string {
	return n.Name + ": " + amountUnit(n)
}

// amountUnit formats just the value of a row (e.g. "29 g", "385 kcal").
func amountUnit(n client.Nutrient) string {
	return trimAmount(n.Amount) + " " + n.Unit
}

// trimAmount drops a trailing ".0" so whole numbers read cleanly; fractional
// amounts ("9.2", "1.1") are returned unchanged.
func trimAmount(s string) string {
	return strings.TrimSuffix(s, ".0")
}

func cmdCategories(args []string) error {
	fs := flag.NewFlagSet("categories", flag.ExitOnError)
	cf := addCommon(fs)
	id := fs.String("id", "", "fetch a single category (with products) by id")
	_ = fs.Parse(reorderArgs(fs, args))
	cl := newClient(cf)
	var raw json.RawMessage
	var err error
	if *id != "" {
		raw, err = cl.Category(*id)
	} else {
		raw, err = cl.Categories()
	}
	if err != nil {
		return err
	}
	if cf.jsonOut || *id != "" {
		return emitRaw(raw)
	}
	// compact human view of the top-level tree
	var tree struct {
		Results []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Categories []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"categories"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return emitRaw(raw)
	}
	for _, top := range tree.Results {
		fmt.Printf("%d  %s\n", top.ID, top.Name)
		for _, sub := range top.Categories {
			fmt.Printf("    %d  %s\n", sub.ID, sub.Name)
		}
	}
	return nil
}

func printHit(indent string, h client.Hit) {
	cat := h.Category()
	if cat != "" {
		cat = "(" + cat + ")"
	}
	fmt.Printf("%s[%s] %s — %s€ %s %s\n", indent, h.ID, h.DisplayName, h.Price.UnitPrice, refFormat(h.Price), cat)
}

func refFormat(p client.PriceInstructions) string {
	if p.ReferencePrice == "" || p.ReferenceFormat == "" {
		return ""
	}
	return fmt.Sprintf("(%s€/%s)", p.ReferencePrice, p.ReferenceFormat)
}

func collectTerms(file string, posArgs []string) ([]string, error) {
	if file == "" {
		return posArgs, nil
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
	var terms []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if t := strings.TrimSpace(sc.Text()); t != "" && !strings.HasPrefix(t, "#") {
			terms = append(terms, t)
		}
	}
	return terms, sc.Err()
}
