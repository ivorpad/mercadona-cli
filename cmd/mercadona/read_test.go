package main

import (
	"reflect"
	"testing"

	"github.com/ivorjpc/mercadona/internal/client"
)

// The exact indentation encodes the parent/sub-nutrient hierarchy a reader relies
// on (Saturadas sits under Grasas), and ".0" trimming is what makes it read like
// the label. Pin both so a formatting tweak is a deliberate, visible change.
func TestNutritionLines(t *testing.T) {
	n := &client.Nutrition{
		PerQuantity:    "Por 100 g",
		EnergyJoules:   client.Nutrient{Name: "Valor", Amount: "1598.0", Unit: "kJ"},
		EnergyCalories: client.Nutrient{Name: "Energético", Amount: "385.0", Unit: "kcal"},
		Nutrients: []client.Nutrient{
			{Name: "Grasas", Amount: "29.0", Unit: "g", SubNutrients: &client.SubNutrients{
				Subtitle: "de las cuales:",
				Items:    []client.Nutrient{{Name: "Saturadas", Amount: "15.0", Unit: "g"}},
			}},
			{Name: "Proteínas", Amount: "9.2", Unit: "g"},
		},
	}
	want := []string{
		"  nutrición (Por 100 g):",
		"    energía: 385 kcal / 1598 kJ",
		"    Grasas: 29 g",
		"      Saturadas: 15 g",
		"    Proteínas: 9.2 g",
	}
	if got := nutritionLines(n); !reflect.DeepEqual(got, want) {
		t.Errorf("nutritionLines mismatch:\n got=%q\nwant=%q", got, want)
	}
	if got := nutritionLines(nil); got != nil {
		t.Errorf("nutritionLines(nil) = %q, want nil", got)
	}
}

// Partial tables exist server-side in principle: only one energy figure, no
// energy at all, an empty per_quantity, or a present-but-rowless table. Pin the
// edges so the render never emits a dangling "/" or a lonely header.
func TestNutritionLinesEdgeCases(t *testing.T) {
	cases := map[string]struct {
		n    *client.Nutrition
		want []string
	}{
		"only kcal": {
			n: &client.Nutrition{
				PerQuantity:    "Por 100 g",
				EnergyCalories: client.Nutrient{Amount: "385.0", Unit: "kcal"},
				Nutrients:      []client.Nutrient{{Name: "Sal", Amount: "1.1", Unit: "g"}},
			},
			want: []string{
				"  nutrición (Por 100 g):",
				"    energía: 385 kcal",
				"    Sal: 1.1 g",
			},
		},
		"only kJ": {
			n: &client.Nutrition{
				EnergyJoules: client.Nutrient{Amount: "1598.0", Unit: "kJ"},
				Nutrients:    []client.Nutrient{{Name: "Sal", Amount: "1.1", Unit: "g"}},
			},
			want: []string{
				"  nutrición:", // no per_quantity → no parenthetical
				"    energía: 1598 kJ",
				"    Sal: 1.1 g",
			},
		},
		"empty table → no lonely header": {
			n:    &client.Nutrition{PerQuantity: "Por 100 g"},
			want: nil,
		},
	}
	for name, c := range cases {
		if got := nutritionLines(c.n); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: nutritionLines mismatch:\n got=%q\nwant=%q", name, got, c.want)
		}
	}
}

func TestTrimAmount(t *testing.T) {
	cases := map[string]string{
		"385.0": "385",
		"29.0":  "29",
		"0.0":   "0",
		"9.2":   "9.2", // fractional left intact
		"1.1":   "1.1",
		"100.0": "100",
		"":      "",
	}
	for in, want := range cases {
		if got := trimAmount(in); got != want {
			t.Errorf("trimAmount(%q) = %q, want %q", in, got, want)
		}
	}
}
