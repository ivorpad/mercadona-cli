package main

import (
	"flag"
	"fmt"

	"github.com/ivorjpc/mercadona/internal/client"
)

// cmdRegulars lists the authenticated customer's "Mis habituales" products in
// Mercadona's relevance order. JSON mode preserves the full upstream response;
// the human view deliberately projects only the fields useful for shopping.
func cmdRegulars(args []string) error {
	fs := flag.NewFlagSet("regulars", flag.ExitOnError)
	cf := addCommon(fs)
	_ = fs.Parse(reorderArgs(fs, args))
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: mercadona regulars [--wh warehouse] [--json]")
	}

	cl, err := authedClient(cf)
	if err != nil {
		return err
	}
	regulars, raw, err := cl.MyRegulars()
	if err != nil {
		return err
	}
	if cf.jsonOut {
		return emitRaw(raw)
	}

	fmt.Printf("habituales: %d productos\n", len(regulars))
	for _, regular := range regulars {
		fmt.Println("  " + formatRegular(regular))
	}
	return nil
}

func formatRegular(regular client.Regular) string {
	product := regular.Product
	line := fmt.Sprintf("[%s] %s", product.ID, orDefault(product.DisplayName, "product "+product.ID))
	if product.Packaging != "" {
		line += " (" + product.Packaging + ")"
	}
	if product.Price.UnitPrice != "" {
		line += " — " + product.Price.UnitPrice + "€"
		if ref := refFormat(product.Price); ref != "" {
			line += " " + ref
		}
	}
	line += "  recomendado: " + fmtQty(regular.RecommendedQuantity)
	if !product.Published {
		line += "  [no disponible]"
	}
	return line
}
