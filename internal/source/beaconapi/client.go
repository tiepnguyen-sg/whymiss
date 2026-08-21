package beaconapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// requestTimeout bounds every single HTTP request this package makes. A
// package constant rather than configuration: I-5 requires a timeout to
// exist, not that its value be tunable per deployment — a caller wanting a
// different ceiling wraps the context it passes in with its own deadline,
// which Client honors via context.Context (min of the two applies).
const requestTimeout = 10 * time.Second

// Client is a read-only handle to one beacon node's standard HTTP API
// (I-1: no request this package makes ever mutates node state).
//
// Client is safe for concurrent use — SSE streaming and REST polling run
// from separate goroutines against the same node.
type Client struct {
	baseURL string
	http    *http.Client
	limiter *rateLimiter
}

// NewClient returns a Client against baseURL (e.g. "http://127.0.0.1:5052").
// minInterval is the minimum spacing enforced between any two requests this
// Client makes — I-5's rate limit — regardless of how many callers are
// issuing requests concurrently.
func NewClient(baseURL string, minInterval time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: requestTimeout},
		limiter: newRateLimiter(minInterval),
	}
}

// get issues a rate-limited GET against path (which must start with "/") and
// decodes the response body's top-level "data" field into out. A 404
// response is reported via the returned bool rather than an error — many
// beacon API endpoints use 404 to mean "not yet available," which callers
// need to distinguish from a real failure.
func (c *Client) get(ctx context.Context, path string, out any) (found bool, err error) {
	if err := c.limiter.wait(ctx); err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return false, fmt.Errorf("build request for %s: %w", path, err)
	}
	return c.do(req, out)
}

// post issues a rate-limited POST with a JSON body against path and decodes
// the response body's top-level "data" field into out.
func (c *Client) post(ctx context.Context, path string, body, out any) (found bool, err error) {
	if err := c.limiter.wait(ctx); err != nil {
		return false, err
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return false, fmt.Errorf("encode request body for %s: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(string(encoded)))
	if err != nil {
		return false, fmt.Errorf("build request for %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) (found bool, err error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails

	// 410 Gone is the attestation pool endpoint's way of saying a slot's data
	// aged out of the pool (pruned, or its epoch moved on) — the window for
	// seeing something published there has closed, the same non-failure
	// meaning as a 404 on a block that was never produced.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096)) //nolint:errcheck // best-effort diagnostic snippet for the error message below; a read failure here just yields an empty snippet
		return false, fmt.Errorf("%s %s: unexpected status %d: %s", req.Method, req.URL.Path, resp.StatusCode, body)
	}
	if out == nil {
		return true, nil
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return false, fmt.Errorf("%s %s: decode response: %w", req.Method, req.URL.Path, err)
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return false, fmt.Errorf("%s %s: decode data field: %w", req.Method, req.URL.Path, err)
	}
	return true, nil
}

// rateLimiter enforces a minimum spacing between successive requests — a
// plain mutex-guarded timestamp rather than a token-bucket package, since
// I-5 only requires *a* ceiling on request rate, not a burst allowance, and
// this needs no new dependency (I-14) to say so.
type rateLimiter struct {
	minInterval time.Duration

	mu   sync.Mutex
	next time.Time
}

func newRateLimiter(minInterval time.Duration) *rateLimiter {
	return &rateLimiter{minInterval: minInterval}
}

// wait blocks until minInterval has elapsed since the previous call's grant,
// or ctx is done, whichever comes first.
func (r *rateLimiter) wait(ctx context.Context) error {
	r.mu.Lock()
	now := time.Now()
	wait := r.next.Sub(now)
	if wait < 0 {
		wait = 0
	}
	r.next = now.Add(wait).Add(r.minInterval)
	r.mu.Unlock()

	if wait == 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
