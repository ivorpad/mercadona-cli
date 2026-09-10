package main

import (
	"testing"

	"github.com/ivorjpc/mercadona/internal/client"
)

func TestFormatRegular(t *testing.T) {
	cases := []struct {
		name string
		in   client.Regular
		want string
	}{
		{
			name: "priced pack",
			in: client.Regular{
				Product: client.ProductView{
					ID:          "27348",
					DisplayName: "Refresco Coca-Cola",
					Packaging:   "Pack-2",
					Published:   true,
					Price: client.PriceInstructions{
						UnitPrice:       "4.10",
						ReferencePrice:  "1.025",
						ReferenceFormat: "L",
					},
				},
				RecommendedQuantity: 3,
			},
			want: "[27348] Refresco Coca-Cola (Pack-2) — 4.10€ (1.025€/L)  recomendado: 3",
		},
		{
			name: "fractional unavailable product without price",
			in: client.Regular{
				Product: client.ProductView{
					ID:          "999",
					DisplayName: "Producto a peso",
					Published:   false,
				},
				RecommendedQuantity: 0.5,
			},
			want: "[999] Producto a peso  recomendado: 0.5  [no disponible]",
		},
		{
			name: "missing name falls back to id",
			in: client.Regular{
				Product:             client.ProductView{ID: "123", Published: true},
				RecommendedQuantity: 1,
			},
			want: "[123] product 123  recomendado: 1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatRegular(tc.in); got != tc.want {
				t.Errorf("formatRegular() = %q, want %q", got, tc.want)
			}
		})
	}
}
