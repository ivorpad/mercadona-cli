package client

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Regular is one product recommendation from the authenticated customer's
// "Mis habituales" list. RecommendedQuantity may be fractional for products
// sold by weight, just like cart quantities.
type Regular struct {
	Product             ProductView `json:"product"`
	RecommendedQuantity float64     `json:"recommended_quantity"`
}

// MyRegulars returns the customer's regular products in Mercadona's relevance
// order, plus the untouched response for --json passthrough.
func (c *Client) MyRegulars() ([]Regular, json.RawMessage, error) {
	if err := c.EnsureCustomer(); err != nil {
		return nil, nil, err
	}
	u := fmt.Sprintf("%s/api/customers/%s/recommendations/myregulars/?wh=%s",
		c.BaseURL, url.PathEscape(c.CustomerID), url.QueryEscape(c.Warehouse))
	var raw json.RawMessage
	if err := c.DoJSON("GET", u, nil, &raw); err != nil {
		return nil, nil, err
	}
	var regulars []Regular
	if err := json.Unmarshal(raw, &regulars); err != nil {
		return nil, raw, fmt.Errorf("decode regulars: %w", err)
	}
	return regulars, raw, nil
}
