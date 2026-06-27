package client

import (
	"encoding/json"
	"testing"
)

// The nutrition table is the value this projection exists to surface, and the
// API shape is fiddly (a list, nested energy rows, optional sub_nutrients). Pin
// the decode so a struct-tag typo or a reshape can't silently drop it.
func TestProductViewNutrition(t *testing.T) {
	// Trimmed to the real product 17559 (empanadilla) payload shape.
	const raw = `{
	  "id":"17559","display_name":"Empanadilla de bacon 11% y queso 32%",
	  "product_information":{
	    "nutritional_information":[
	      {"per_quantity":"Por 100 g",
	       "energy_joules":{"name":"Valor","amount":"1598.0","unit":"kJ"},
	       "energy_calories":{"name":"Energético","amount":"385.0","unit":"kcal"},
	       "nutrients":[
	         {"name":"Grasas","amount":"29.0","unit":"g","sub_nutrients":{"subtitle":"de las cuales:","items":[{"name":"Saturadas","amount":"15.0","unit":"g"}]}},
	         {"name":"Proteínas","amount":"9.2","unit":"g","sub_nutrients":null}
	       ],
	       "accessible_text":"Por 100 gramos..."}
	    ]
	  }
	}`
	var pv ProductView
	if err := json.Unmarshal([]byte(raw), &pv); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	n := pv.Nutrition()
	if n == nil {
		t.Fatal("Nutrition() = nil, want a table")
	}
	if n.PerQuantity != "Por 100 g" {
		t.Errorf("PerQuantity = %q, want %q", n.PerQuantity, "Por 100 g")
	}
	if n.EnergyCalories.Amount != "385.0" || n.EnergyCalories.Unit != "kcal" {
		t.Errorf("EnergyCalories = %q %q, want 385.0 kcal", n.EnergyCalories.Amount, n.EnergyCalories.Unit)
	}
	if len(n.Nutrients) != 2 {
		t.Fatalf("len(Nutrients) = %d, want 2", len(n.Nutrients))
	}
	g := n.Nutrients[0]
	if g.Name != "Grasas" || g.SubNutrients == nil {
		t.Fatalf("Nutrients[0] = %+v, want Grasas with sub_nutrients", g)
	}
	if len(g.SubNutrients.Items) != 1 || g.SubNutrients.Items[0].Name != "Saturadas" || g.SubNutrients.Items[0].Amount != "15.0" {
		t.Errorf("Grasas sub_nutrients = %+v, want [Saturadas 15.0]", g.SubNutrients.Items)
	}
	if n.Nutrients[1].SubNutrients != nil {
		t.Errorf("Proteínas SubNutrients = %+v, want nil", n.Nutrients[1].SubNutrients)
	}
}

// Most products (staples) come back with product_information null or absent, and
// some callers hold a nil view — Nutrition() must report "no table", not panic.
func TestProductViewNutritionAbsent(t *testing.T) {
	for name, raw := range map[string]string{
		"null":   `{"id":"6245","product_information":null}`,
		"absent": `{"id":"6245"}`,
		"empty":  `{"id":"6245","product_information":{"nutritional_information":[]}}`,
	} {
		var pv ProductView
		if err := json.Unmarshal([]byte(raw), &pv); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if got := pv.Nutrition(); got != nil {
			t.Errorf("%s: Nutrition() = %+v, want nil", name, got)
		}
	}
	if got := (*ProductView)(nil).Nutrition(); got != nil {
		t.Errorf("nil receiver: Nutrition() = %+v, want nil", got)
	}
}
