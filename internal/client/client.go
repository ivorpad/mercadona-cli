// Package client is a thin, dependency-free Go client for the (unofficial)
// tienda.mercadona.es API plus its Algolia product search. It speaks the same
// HTTP shapes the web app does so Akamai keeps it in monitor mode.
package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// BaseURL is the web/app shared API host.
	BaseURL = "https://tienda.mercadona.es"
	// DefaultUA mirrors a current desktop Chrome so requests look like the web app.
	DefaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
	// DefaultVersion is the x-version header; it tracks the deployed SPA bundle
	// (e.g. /v9200/...). Overridable, and refreshed alongside Algolia creds.
	DefaultVersion = "v9200"
)

// Client is the API entrypoint. The zero value is not usable — use New.
type Client struct {
	HTTP       *http.Client
	BaseURL    string
	UserAgent  string
	Version    string
	DeviceID   string
	Warehouse  string // e.g. "mad1"
	Lang       string // e.g. "es"
	Token      string // bearer, empty for anonymous (read/search) calls
	Cookie     string // optional raw Cookie header (Akamai clearance from a browser session)
	CustomerID string // resolved from the token's customer_uuid

	// Credentials for transparent re-auth when the access token expires.
	Username     string
	Password     string
	RefreshToken string
	reauthing    bool // guard against recursion while refreshing/logging in

	Algolia AlgoliaCreds // search creds; populated lazily by EnsureAlgolia
}

// New returns a Client with web-app-like defaults (anonymous, Madrid warehouse).
// Its HTTP client presents Chrome's TLS (JA3) fingerprint via uTLS; if that
// fails to initialize it falls back to the stdlib transport.
func New() *Client {
	hc := &http.Client{Timeout: 30 * time.Second}
	if tr, err := newChromeTransport(); err == nil {
		hc.Transport = tr
	}
	return &Client{
		HTTP:      hc,
		BaseURL:   BaseURL,
		UserAgent: DefaultUA,
		Version:   DefaultVersion,
		DeviceID:  "00000000-0000-0000-0000-000000000000",
		Warehouse: "mad1",
		Lang:      "es",
		// CustomerID is resolved from the token's customer_uuid (EnsureCustomer);
		// the literal "me" alias is rejected by the API with 403.
	}
}

// APIError carries a non-2xx response so callers (and agents) can branch on it.
// RetryAfter is the parsed Retry-After header on a 429/503 (>=0 when the server
// sent one, -1 when absent), used to pace automatic backoff.
type APIError struct {
	Status     int
	Body       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("mercadona api: HTTP %d: %s", e.Status, truncate(e.Body, 300))
}

func (c *Client) newReq(method, url string, body any) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("accept-language", "es-ES,es;q=0.9,en;q=0.8")
	req.Header.Set("user-agent", c.UserAgent)
	req.Header.Set("referer", c.BaseURL+"/")
	req.Header.Set("origin", c.BaseURL)
	req.Header.Set("x-version", c.Version)
	if c.DeviceID != "" {
		req.Header.Set("x-customer-device-id", c.DeviceID)
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("authorization", "Bearer "+c.Token)
	}
	if c.Cookie != "" {
		req.Header.Set("cookie", c.Cookie)
	}
	return req, nil
}

// Automatic backoff on throttling (HTTP 429/503): retry up to maxRetries times,
// honouring Retry-After, else exponential backoff with jitter, each wait capped.
const (
	maxRetries       = 3
	defaultRetryBase = 500 * time.Millisecond
	maxRetryWait     = 10 * time.Second
)

// isRateLimited reports whether err is a throttling response worth backing off on.
func isRateLimited(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && (ae.Status == http.StatusTooManyRequests || ae.Status == http.StatusServiceUnavailable)
}

// parseRetryAfter reads the delta-seconds form of Retry-After; -1 when absent or in
// the (rare here) HTTP-date form, so the caller falls back to its own backoff.
func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return -1
	}
	if secs, err := strconv.Atoi(h); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return -1
}

// retryWait is how long to wait before the next attempt: the server's Retry-After
// if it sent one, else exponential backoff plus jitter — both capped.
func retryWait(err error, backoff time.Duration) time.Duration {
	var ae *APIError
	if errors.As(err, &ae) && ae.RetryAfter >= 0 {
		return capWait(ae.RetryAfter)
	}
	return capWait(backoff + time.Duration(rand.Int63n(int64(250*time.Millisecond))))
}

func capWait(d time.Duration) time.Duration {
	if d > maxRetryWait {
		return maxRetryWait
	}
	if d < 0 {
		return 0
	}
	return d
}

// doWithBackoff issues a request, retrying transient throttling (429/503) so a burst
// of calls — e.g. pricing a fresh basket, or many searches during a shop — degrades
// gracefully instead of failing. Non-throttle errors return immediately.
func (c *Client) doWithBackoff(method, url string, body, out any) error {
	backoff := defaultRetryBase
	for attempt := 0; ; attempt++ {
		err := c.doOnce(method, url, body, out)
		if !isRateLimited(err) || attempt >= maxRetries {
			return err
		}
		time.Sleep(retryWait(err, backoff))
		backoff *= 2
	}
}

// DoJSON performs an API request with automatic throttle backoff and, if it fails
// with an expired-token 401 and we have credentials, transparently re-authenticates
// once and retries.
func (c *Client) DoJSON(method, url string, body, out any) error {
	err := c.doWithBackoff(method, url, body, out)
	if c.shouldReauth(err) {
		c.reauthing = true
		rerr := c.reauth()
		c.reauthing = false
		if rerr == nil {
			return c.doWithBackoff(method, url, body, out)
		}
	}
	return err
}

func (c *Client) shouldReauth(err error) bool {
	if c.reauthing || !c.CanReauth() {
		return false
	}
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized && strings.Contains(ae.Body, "token_not_valid")
}

// doOnce performs a single API request against BaseURL, decoding a 2xx JSON body
// into out (if non-nil). Non-2xx responses become *APIError.
func (c *Client) doOnce(method, url string, body, out any) error {
	req, err := c.newReq(method, url, body)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Body: string(data), RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode %s: %w", url, err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
