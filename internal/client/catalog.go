package client

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// These endpoints are anonymous reads (no token, no Akamai clearance needed at
// human-paced volume). They return the raw API JSON for passthrough, plus we
// offer a projected view for product detail.

// Categories returns the warehouse category tree (raw JSON).
func (c *Client) Categories() (json.RawMessage, error) {
	u := fmt.Sprintf("%s/api/categories/?lang=%s&wh=%s", c.BaseURL, c.Lang, url.QueryEscape(c.Warehouse))
	var raw json.RawMessage
	return raw, c.DoJSON("GET", u, nil, &raw)
}

// Category returns a single category (with its products/subcategories), raw JSON.
func (c *Client) Category(id string) (json.RawMessage, error) {
	u := fmt.Sprintf("%s/api/categories/%s/?lang=%s&wh=%s", c.BaseURL, url.PathEscape(id), c.Lang, url.QueryEscape(c.Warehouse))
	var raw json.RawMessage
	return raw, c.DoJSON("GET", u, nil, &raw)
}

// ProductView is the projected detail we surface for human output.
type ProductView struct {
	ID          string            `json:"id"`
	Slug        string            `json:"slug"`
	DisplayName string            `json:"display_name"`
	Packaging   string            `json:"packaging"`
	ShareURL    string            `json:"share_url"`
	Price       PriceInstructions `json:"price_instructions"`
	// ProductInformation.NutritionalInformation is the per-quantity nutrition
	// table; the API only returns it when the request carries an x-version at or
	// above the SPA bundle that introduced it (we already send DefaultVersion),
	// and only for products that have it filled in (mostly prepared/processed
	// items — most staples come back without one).
	ProductInformation struct {
		NutritionalInformation []Nutrition `json:"nutritional_information"`
	} `json:"product_information"`
}

// Nutrition is one per-quantity nutritional table (e.g. "Por 100 g") as returned
// under product_information.nutritional_information.
type Nutrition struct {
	PerQuantity    string     `json:"per_quantity"`
	EnergyJoules   Nutrient   `json:"energy_joules"`
	EnergyCalories Nutrient   `json:"energy_calories"`
	Nutrients      []Nutrient `json:"nutrients"`
	AccessibleText string     `json:"accessible_text"`
}

// Nutrient is one labelled amount — a top-level row (Grasas, Proteínas, Sal…),
// an energy figure, or a sub-row (Saturadas, Azúcares). SubNutrients is set only
// on rows that break down further.
type Nutrient struct {
	Name         string        `json:"name"`
	Amount       string        `json:"amount"`
	Unit         string        `json:"unit"`
	SubNutrients *SubNutrients `json:"sub_nutrients,omitempty"`
}

// SubNutrients is the "de las cuales:" breakdown nested under a Nutrient.
type SubNutrients struct {
	Subtitle string     `json:"subtitle"`
	Items    []Nutrient `json:"items"`
}

// Nutrition returns the product's primary nutrition table (the first the API
// lists, i.e. the per-100g/ml one the website shows), or nil when the API
// supplied none — which is the common case for staples.
func (pv *ProductView) Nutrition() *Nutrition {
	if pv == nil || len(pv.ProductInformation.NutritionalInformation) == 0 {
		return nil
	}
	return &pv.ProductInformation.NutritionalInformation[0]
}

// Product returns both the projected view and the full raw JSON for a product.
func (c *Client) Product(id string) (*ProductView, json.RawMessage, error) {
	u := fmt.Sprintf("%s/api/products/%s/?lang=%s&wh=%s", c.BaseURL, url.PathEscape(id), c.Lang, url.QueryEscape(c.Warehouse))
	var raw json.RawMessage
	if err := c.DoJSON("GET", u, nil, &raw); err != nil {
		return nil, nil, err
	}
	var pv ProductView
	_ = json.Unmarshal(raw, &pv) // projection is best-effort
	return &pv, raw, nil
}
