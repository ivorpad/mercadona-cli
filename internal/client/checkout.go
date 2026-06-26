package client

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func (c *Client) custURL(suffix string) string {
	return fmt.Sprintf("%s/api/customers/%s/%s", c.BaseURL, c.CustomerID, suffix)
}

// Addresses lists the customer's saved delivery addresses.
func (c *Client) Addresses() (json.RawMessage, error) {
	var raw json.RawMessage
	return raw, c.DoJSON("GET", c.custURL("addresses/"), nil, &raw)
}

// Slots lists delivery slots for an address. Slots live under the ADDRESS, not
// the checkout (GET /api/customers/<cid>/addresses/<addrId>/slots/), and the
// response is {next_page, results:[{id,start,end,price,available,open,...}]}.
func (c *Client) Slots(addressID int) (json.RawMessage, error) {
	var raw json.RawMessage
	return raw, c.DoJSON("GET", c.custURL(fmt.Sprintf("addresses/%d/slots/", addressID)), nil, &raw)
}

// CreateCheckout opens a checkout from the current cart. The response carries
// the checkout id plus the default address (raw JSON); delivery slots are
// fetched separately via Slots, since they hang off the address.
func (c *Client) CreateCheckout(cart *Cart) (json.RawMessage, error) {
	body := map[string]any{"cart": map[string]any{
		"id": cart.ID, "version": cart.Version, "lines": cart.Lines,
	}}
	var raw json.RawMessage
	return raw, c.DoJSON("POST", c.custURL("checkouts/"), body, &raw)
}

// SetDelivery attaches a delivery address + slot to an open checkout.
func (c *Client) SetDelivery(checkoutID string, addressID int, slotID string) (json.RawMessage, error) {
	body := map[string]any{
		"address": map[string]any{"id": addressID},
		"slot":    map[string]any{"id": slotID},
	}
	var raw json.RawMessage
	return raw, c.DoJSON("PUT", c.custURL("checkouts/"+checkoutID+"/delivery-info/"), body, &raw)
}

// GetCheckout reads an open checkout — used to read its authoritative total (incl.
// delivery) right before the irreversible submit, for the budget guard.
func (c *Client) GetCheckout(checkoutID string) (json.RawMessage, error) {
	var raw json.RawMessage
	return raw, c.DoJSON("GET", c.custURL("checkouts/"+checkoutID+"/"), nil, &raw)
}

// SubmitOrder places the order. This is IRREVERSIBLE and spends money — callers
// MUST gate it behind explicit user consent.
func (c *Client) SubmitOrder(checkoutID string) (json.RawMessage, error) {
	var raw json.RawMessage
	return raw, c.DoJSON("POST", c.custURL("checkouts/"+checkoutID+"/orders/"), nil, &raw)
}

// money parses a price the API sends as either a JSON string ("76.84") or a
// number, into euros.
type money float64

func (m *money) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil // tolerate unexpected shapes; ExtractTotal then reports "not found"
	}
	*m = money(f)
	return nil
}

// ExtractTotal pulls the order/cart total (in €) out of a cart or checkout JSON
// response, tolerating the shapes the API uses (summary.total, price.total, or a
// top-level total). Returns false when no positive total could be read.
func ExtractTotal(raw json.RawMessage) (float64, bool) {
	var v struct {
		Total   money `json:"total"`
		Summary struct {
			Total money `json:"total"`
		} `json:"summary"`
		Price struct {
			Total money `json:"total"`
		} `json:"price"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return 0, false
	}
	for _, t := range []money{v.Summary.Total, v.Price.Total, v.Total} {
		if t > 0 {
			return float64(t), true
		}
	}
	return 0, false
}
