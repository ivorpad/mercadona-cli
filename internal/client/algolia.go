package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ivorjpc/mercadona/internal/config"
)

// AlgoliaCreds are the public, search-only credentials embedded in the SPA
// bundle. The App ID rotates (observed 7UZJKL1GNI -> 7UZJKL1DJ0), so a shipped
// CLI must be able to re-discover them rather than hardcode — see Discover.
type AlgoliaCreds struct {
	AppID     string `json:"app_id"`
	APIKey    string `json:"api_key"`
	IndexBase string `json:"index_base"` // "products_prod"
}

const algoliaCacheFile = "algolia.json"

// fallbackAlgolia is the last-known-good config, used before discovery so the
// common case needs zero extra round-trips. Refreshed automatically on failure.
var fallbackAlgolia = AlgoliaCreds{
	AppID:     "7UZJKL1DJ0",
	APIKey:    "9d8f2e39e90df472b4f2e559a116fe17",
	IndexBase: "products_prod",
}

var (
	// matches the hashed SPA entry bundle, e.g. /v9200/index-DdAHguc-.js
	reBundlePath = regexp.MustCompile(`/v\d+/index-[A-Za-z0-9_-]+\.js`)
	// in the bundle the app id and 32-hex search key sit adjacent:
	//   kGe="7UZJKL1DJ0",BGe="9d8f...17",DGe="products_prod"
	reAlgoliaPair = regexp.MustCompile(`"([A-Z0-9]{10})",[A-Za-z0-9_$]+="([0-9a-f]{32})"`)
	reIndexBase   = regexp.MustCompile(`"(products_prod[a-z_]*)"`)
)

// IndexName is the per-warehouse, per-language Algolia index, e.g.
// products_prod_mad1_es.
func (c *Client) IndexName() string {
	return c.Algolia.IndexBase + "_" + c.Warehouse + "_" + c.Lang
}

// EnsureAlgolia populates c.Algolia from (in order): an existing value, the
// on-disk cache, or the compiled-in fallback. It never hits the network.
func (c *Client) EnsureAlgolia() {
	if c.Algolia.AppID != "" {
		return
	}
	var cached AlgoliaCreds
	if err := config.Load(algoliaCacheFile, &cached); err == nil && cached.AppID != "" {
		c.Algolia = cached
		return
	}
	c.Algolia = fallbackAlgolia
}

// RefreshAlgolia scrapes the live SPA bundle for current creds and caches them.
func (c *Client) RefreshAlgolia() error {
	creds, err := DiscoverAlgolia(c.HTTP, c.BaseURL, c.UserAgent)
	if err != nil {
		return err
	}
	c.Algolia = creds
	_ = config.Save(algoliaCacheFile, creds) // best-effort cache
	return nil
}

// DiscoverAlgolia fetches the SPA shell, locates the entry bundle, and extracts
// the current Algolia app id / search key / index base from it.
func DiscoverAlgolia(httpc *http.Client, baseURL, ua string) (AlgoliaCreds, error) {
	shell, err := fetchText(httpc, baseURL+"/", ua)
	if err != nil {
		return AlgoliaCreds{}, fmt.Errorf("fetch shell: %w", err)
	}
	path := reBundlePath.FindString(shell)
	if path == "" {
		return AlgoliaCreds{}, fmt.Errorf("entry bundle not found in SPA shell")
	}
	js, err := fetchText(httpc, baseURL+path, ua)
	if err != nil {
		return AlgoliaCreds{}, fmt.Errorf("fetch bundle %s: %w", path, err)
	}
	pair := reAlgoliaPair.FindStringSubmatch(js)
	if pair == nil {
		return AlgoliaCreds{}, fmt.Errorf("algolia creds not found in bundle %s", path)
	}
	creds := AlgoliaCreds{AppID: pair[1], APIKey: pair[2], IndexBase: "products_prod"}
	if ib := reIndexBase.FindStringSubmatch(js); ib != nil {
		creds.IndexBase = ib[1]
	}
	return creds, nil
}

// SearchResult is the subset of an Algolia query response we expose.
type SearchResult struct {
	Query  string `json:"query"`
	NbHits int    `json:"nbHits"`
	Hits   []Hit  `json:"hits"`
}

// Hit is a projected product from search (a tiny slice of Algolia's fat record).
type Hit struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Packaging   string `json:"packaging"`
	Thumbnail   string `json:"thumbnail"`
	ShareURL    string `json:"share_url"`
	Categories  []struct {
		Name string `json:"name"`
	} `json:"categories"`
	Price PriceInstructions `json:"price_instructions"`
}

// PriceInstructions is the pricing block shared by search hits and product detail.
type PriceInstructions struct {
	UnitPrice       string `json:"unit_price"`
	BulkPrice       string `json:"bulk_price"`
	ReferencePrice  string `json:"reference_price"`
	ReferenceFormat string `json:"reference_format"`
	SizeFormat      string `json:"size_format"`
	UnitName        string `json:"unit_name"`
	IsPack          bool   `json:"is_pack"`
}

// Category returns the first category name, or "" — handy for compact output.
func (h Hit) Category() string {
	if len(h.Categories) == 0 {
		return ""
	}
	return h.Categories[0].Name
}

// Search runs a single full-text query against the warehouse index. Optional
// facetFilters (e.g. "categories.id:-17" to exclude frozen) are AND-ed together.
// On an auth/not-found error (a sign the app id rotated) it refreshes creds once
// and retries.
func (c *Client) Search(query string, hitsPerPage int, facetFilters ...string) (*SearchResult, error) {
	c.EnsureAlgolia()
	res, err := c.searchOnce(query, hitsPerPage, facetFilters)
	if err != nil && shouldRefresh(err) {
		if rerr := c.RefreshAlgolia(); rerr == nil {
			return c.searchOnce(query, hitsPerPage, facetFilters)
		}
	}
	return res, err
}

func (c *Client) searchOnce(query string, hitsPerPage int, facetFilters []string) (*SearchResult, error) {
	body := map[string]string{"params": algoliaParams(query, hitsPerPage, facetFilters)}
	var out SearchResult
	if err := c.algoliaPost("/1/indexes/"+c.IndexName()+"/query", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Batch runs many queries in a single multi-index request (100 items ≈ 1 call).
// Results are returned aligned to the input order. Optional facetFilters apply to
// every query (e.g. "categories.id:-17" so no term's top hit is a frozen item).
func (c *Client) Batch(queries []string, hitsEach int, facetFilters ...string) ([]SearchResult, error) {
	c.EnsureAlgolia()
	res, err := c.batchOnce(queries, hitsEach, facetFilters)
	if err != nil && shouldRefresh(err) {
		if rerr := c.RefreshAlgolia(); rerr == nil {
			return c.batchOnce(queries, hitsEach, facetFilters)
		}
	}
	return res, err
}

func (c *Client) batchOnce(queries []string, hitsEach int, facetFilters []string) ([]SearchResult, error) {
	idx := c.IndexName()
	type reqItem struct {
		IndexName string `json:"indexName"`
		Params    string `json:"params"`
	}
	reqs := make([]reqItem, len(queries))
	for i, q := range queries {
		reqs[i] = reqItem{IndexName: idx, Params: algoliaParams(q, hitsEach, facetFilters)}
	}
	var out struct {
		Results []SearchResult `json:"results"`
	}
	if err := c.algoliaPost("/1/indexes/*/queries", map[string]any{"requests": reqs}, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

func algoliaParams(query string, hitsPerPage int, facetFilters []string) string {
	v := url.Values{}
	v.Set("query", query)
	v.Set("hitsPerPage", fmt.Sprintf("%d", hitsPerPage))
	if len(facetFilters) > 0 {
		// Algolia wants facetFilters as a JSON array string; a flat array ANDs the
		// entries. url.Values.Encode handles the percent-escaping.
		if b, err := json.Marshal(facetFilters); err == nil {
			v.Set("facetFilters", string(b))
		}
	}
	return v.Encode()
}

func (c *Client) algoliaPost(path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := "https://" + strings.ToLower(c.Algolia.AppID) + "-dsn.algolia.net" + path
	backoff := defaultRetryBase
	for attempt := 0; ; attempt++ {
		err = c.algoliaPostOnce(url, b, out)
		if !isRateLimited(err) || attempt >= maxRetries {
			return err
		}
		time.Sleep(retryWait(err, backoff))
		backoff *= 2
	}
}

func (c *Client) algoliaPostOnce(url string, b []byte, out any) error {
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("X-Algolia-API-Key", c.Algolia.APIKey)
	req.Header.Set("X-Algolia-Application-Id", c.Algolia.AppID)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("user-agent", c.UserAgent)
	req.Header.Set("referer", c.BaseURL+"/")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Body: string(data), RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	return json.Unmarshal(data, out)
}

// shouldRefresh reports whether err looks like stale creds: an Algolia auth/
// not-found status, or DNS failure from a rotated (now-dead) app-id host.
func shouldRefresh(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return true
		}
	}
	return strings.Contains(err.Error(), "no such host")
}

func fetchText(httpc *http.Client, url, ua string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("user-agent", ua)
	req.Header.Set("accept", "*/*")
	resp, err := httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Status: resp.StatusCode, Body: url}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	return string(b), err
}
