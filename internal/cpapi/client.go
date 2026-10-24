// Package cpapi is a typed Go client for the Interactive Brokers
// Client Portal Web API 1.0 (CPAPI 1.0). Endpoint paths, parameter
// names, and rate limits follow IBKR's official Web API
// documentation and were cross-checked against the IBind Python
// client (Voyz/ibind).
//
// The client is transport-agnostic: cmd wires it with the
// AuthProvider's base URL and RoundTripper (plain loopback transport
// in gateway mode, OAuth-signing transport in OAuth mode), so every
// endpoint works identically in both modes.
package cpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rayli09/ibkr-cli/internal/apierr"
)

// DefaultTimeout for API calls.
const DefaultTimeout = 30 * time.Second

// Response is the result of an API call: status, headers, raw body.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// IsError reports whether the status code indicates an error.
func (r *Response) IsError() bool { return r.StatusCode >= 400 }

// JSON decodes the response body into v.
func (r *Response) JSON(v any) error { return json.Unmarshal(r.Body, v) }

// Client speaks to one CPAPI base URL through one transport.
type Client struct {
	BaseURL   string
	Transport http.RoundTripper // nil = http.DefaultTransport
	Timeout   time.Duration

	limiter *rateLimiter

	// Snapshot priming (§7.1): delay between the priming request and
	// the real one. Injectable for tests.
	PrimeDelay time.Duration
	// Sleep is injectable for tests (rate-limit pacing, priming).
	Sleep func(time.Duration)
	// Now is injectable for tests (tickle bookkeeping).
	Now func() time.Time

	mu         sync.Mutex
	lastTickle time.Time
}

// New builds a Client.
func New(baseURL string, transport http.RoundTripper) *Client {
	return &Client{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		Transport:  transport,
		limiter:    newRateLimiter(),
		PrimeDelay: 700 * time.Millisecond,
	}
}

func (c *Client) sleep(d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) httpClient() *http.Client {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{Transport: c.Transport, Timeout: timeout}
}

// routeOf maps a request path to its rate-limit route key (SPEC §2).
func routeOf(path string) string {
	switch {
	case strings.HasSuffix(path, "/iserver/marketdata/snapshot"):
		return "snapshot"
	case strings.HasSuffix(path, "/iserver/marketdata/history"):
		return "history"
	case strings.HasPrefix(path, "/hmds/"):
		return "history"
	case strings.HasSuffix(path, "/iserver/scanner/params"), strings.HasSuffix(path, "/hmds/scanner/params"):
		return "scannerparams"
	case strings.HasSuffix(path, "/iserver/scanner/run"), strings.HasSuffix(path, "/hmds/scanner/run"):
		return "scannerrun"
	case strings.HasPrefix(path, "/pa/"):
		return "pa"
	case strings.HasSuffix(path, "/iserver/account/orders"):
		return "orders"
	case strings.HasSuffix(path, "/iserver/account/trades"):
		return "trades"
	case strings.HasSuffix(path, "/iserver/account/pnl/partitioned"):
		return "pnl"
	case strings.HasPrefix(path, "/portfolio"):
		return "portfolio"
	case strings.HasSuffix(path, "/tickle"):
		return "tickle"
	case strings.HasSuffix(path, "/sso/validate"):
		return "validate"
	default:
		return "default"
	}
}

// Do performs one API request with rate limiting, automatic session
// tickle for /iserver paths, and bounded retries on HTTP 429.
func (c *Client) Do(ctx context.Context, method, path string,
	query url.Values, jsonBody any) (*Response, error) {
	if strings.HasPrefix(path, "/iserver/") {
		if err := c.maybeTickle(ctx); err != nil {
			return nil, err
		}
	}
	route := routeOf(path)
	// The limiter is acquired once per logical request: a 429 retry
	// is paced by Retry-After, not by the per-route interval (which
	// for /sso/validate is a full minute and would make retries
	// useless). For history the release also holds a concurrency
	// slot, kept across retries and freed when Do returns.
	release, err := c.limiter.acquire(ctx, route)
	if err != nil {
		return nil, err
	}
	defer release()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := c.doOnce(ctx, method, path, query, jsonBody)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = &apierr.RateLimitError{Route: route}
			wait := retryAfter(resp.Header)
			if attempt < 2 {
				c.sleep(wait)
				continue
			}
			return nil, lastErr
		}
		return resp, respError(resp)
	}
	return nil, lastErr
}

func retryAfter(h http.Header) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		var secs int
		if _, err := fmt.Sscanf(v, "%d", &secs); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return time.Second
}

// respError converts an HTTP error response into a typed error.
// 401/403 mean the brokerage session is not live (SPEC §3).
func respError(resp *Response) error {
	if !resp.IsError() {
		return nil
	}
	body := strings.TrimSpace(string(resp.Body))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		msg := "session not authenticated (HTTP " + fmt.Sprint(resp.StatusCode) + ")"
		if body != "" {
			msg += ": " + body
		}
		return &apierr.SessionError{Msg: msg}
	}
	return &apierr.APIError{StatusCode: resp.StatusCode, Body: body}
}

func (c *Client) doOnce(ctx context.Context, method, path string,
	query url.Values, jsonBody any) (*Response, error) {
	u := c.BaseURL + path
	var bodyBytes []byte
	if jsonBody != nil {
		data, err := json.Marshal(jsonBody)
		if err != nil {
			return nil, err
		}
		bodyBytes = data
	}
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if bodyBytes != nil {
		body = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// CPAPI convention (the gateway rejects some calls without it).
	req.Header.Set("User-Agent", "ibkr-cli")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, &apierr.APIError{Msg: fmt.Sprintf("request failed: %v", err)}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.lastTickle = c.now() // any successful contact refreshes the idle clock
	c.mu.Unlock()
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: data}, nil
}

// maybeTickle POSTs /tickle when the last session contact is more
// than 5 minutes old (SPEC §3: sessions decay in minutes).
func (c *Client) maybeTickle(ctx context.Context) error {
	c.mu.Lock()
	stale := c.now().Sub(c.lastTickle) > 5*time.Minute
	c.mu.Unlock()
	if !stale {
		return nil
	}
	release, err := c.limiter.acquire(ctx, "tickle")
	if err != nil {
		return err
	}
	resp, err := c.doOnce(ctx, http.MethodPost, "/tickle", nil, nil)
	release()
	if err != nil {
		return err
	}
	return respError(resp)
}

// Get is Do for a read.
func (c *Client) Get(ctx context.Context, path string, query url.Values) (*Response, error) {
	return c.Do(ctx, http.MethodGet, path, query, nil)
}

// Post is Do with a JSON body.
func (c *Client) Post(ctx context.Context, path string, body any) (*Response, error) {
	return c.Do(ctx, http.MethodPost, path, nil, body)
}

// Delete is Do for a delete.
func (c *Client) Delete(ctx context.Context, path string) (*Response, error) {
	return c.Do(ctx, http.MethodDelete, path, nil, nil)
}

// DeleteQ is Do for a delete with query parameters.
func (c *Client) DeleteQ(ctx context.Context, path string, query url.Values) (*Response, error) {
	return c.Do(ctx, http.MethodDelete, path, query, nil)
}

// setParam sets a query parameter only when value is non-empty.
func setParam(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}
