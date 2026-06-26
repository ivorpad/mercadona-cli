package client

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// seqRT returns the next status code from codes on each call (200 once exhausted).
// Throttle codes carry Retry-After: 0 so the backoff loop doesn't actually sleep.
type seqRT struct {
	codes []int
	calls int
}

func (s *seqRT) RoundTrip(*http.Request) (*http.Response, error) {
	code := 200
	if s.calls < len(s.codes) {
		code = s.codes[s.calls]
	}
	s.calls++
	h := http.Header{}
	if code == http.StatusTooManyRequests || code == http.StatusServiceUnavailable {
		h.Set("Retry-After", "0")
	}
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Header:     h,
	}, nil
}

func testClient(rt http.RoundTripper) *Client {
	c := New()
	c.HTTP = &http.Client{Transport: rt}
	return c
}

func TestDoJSONRetriesOnRateLimit(t *testing.T) {
	rt := &seqRT{codes: []int{429, 503, 200}}
	c := testClient(rt)
	var out map[string]any
	if err := c.DoJSON("GET", "https://x/api/", nil, &out); err != nil {
		t.Fatalf("expected success after backoff, got %v", err)
	}
	if rt.calls != 3 {
		t.Fatalf("expected 3 attempts (429,503,200), got %d", rt.calls)
	}
}

func TestDoJSONGivesUpAfterMaxRetries(t *testing.T) {
	rt := &seqRT{codes: []int{429, 429, 429, 429, 429}}
	c := testClient(rt)
	err := c.DoJSON("GET", "https://x/api/", nil, nil)
	if !isRateLimited(err) {
		t.Fatalf("expected a rate-limit error after exhausting retries, got %v", err)
	}
	if rt.calls != maxRetries+1 {
		t.Fatalf("expected %d attempts, got %d", maxRetries+1, rt.calls)
	}
}

func TestDoJSONDoesNotRetryOn4xx(t *testing.T) {
	rt := &seqRT{codes: []int{400, 200}}
	c := testClient(rt)
	err := c.DoJSON("GET", "https://x/api/", nil, nil)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("expected an immediate 400, got %v", err)
	}
	if rt.calls != 1 {
		t.Fatalf("a 400 must not be retried; got %d calls", rt.calls)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("2"); d != 2*time.Second {
		t.Fatalf("'2' → %v, want 2s", d)
	}
	if d := parseRetryAfter("0"); d != 0 {
		t.Fatalf("'0' → %v, want 0", d)
	}
	if d := parseRetryAfter(""); d != -1 {
		t.Fatalf("absent → %v, want -1 (sentinel)", d)
	}
	if d := parseRetryAfter("Wed, 21 Oct 2026 07:28:00 GMT"); d != -1 {
		t.Fatalf("HTTP-date → %v, want -1 (fall back to backoff)", d)
	}
}

func TestIsRateLimited(t *testing.T) {
	if !isRateLimited(&APIError{Status: 429}) || !isRateLimited(&APIError{Status: 503}) {
		t.Fatal("429/503 should be rate-limited")
	}
	if isRateLimited(&APIError{Status: 500}) || isRateLimited(&APIError{Status: 403}) || isRateLimited(nil) {
		t.Fatal("500/403/nil should not be rate-limited")
	}
}
