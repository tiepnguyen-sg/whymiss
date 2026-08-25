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

// streamTimeout bounds silence on an SSE connection before reconnecting.
const streamTimeout = 90 * time.Second

const (
	maxResponseBodyBytes = 16 << 20
	maxRESTConnections   = 4
)

// Client is a read-only handle to one beacon node's standard HTTP API
// (I-1: no request this package makes ever mutates node state).
//
// Client is safe for concurrent use — SSE streaming and REST polling run
// from separate goroutines against the same node.
type Client struct {
	baseURL           string
	http              *http.Client
	streamHTTP        *http.Client
	streamIdleTimeout time.Duration
	limiter           *rateLimiter

	blockMu                   sync.Mutex
	blockCache                map[uint64]*blockCacheEntry
	blockAttestationsEndpoint endpointSupport

	rootMu         sync.Mutex
	canonicalRoots map[uint64]*canonicalRootEntry

	headMu    sync.Mutex
	headPolls map[uint64]*headPollEntry

	latestHeadMu sync.Mutex
	latestHead   *latestHeadEntry

	committeeMu    sync.Mutex
	committeeCache map[uint64]*committeeCacheEntry

	// blockRecoveryBudget overrides defaultBlockRecoveryBudget; zero means
	// use the default. Tests set it small to assert the give-up path
	// without waiting for it.
	blockRecoveryBudget time.Duration
}

// NewClient returns a Client against baseURL (e.g. "http://127.0.0.1:5052").
// minInterval is the minimum spacing enforced between any two requests this
// Client makes — I-5's rate limit — regardless of how many callers are
// issuing requests concurrently.
func NewClient(baseURL string, minInterval time.Duration) *Client {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		defaultTransport = &http.Transport{}
	}
	restTransport := defaultTransport.Clone()
	restTransport.Proxy = nil
	restTransport.MaxIdleConns = maxRESTConnections
	restTransport.MaxIdleConnsPerHost = maxRESTConnections
	restTransport.MaxConnsPerHost = maxRESTConnections
	restTransport.ResponseHeaderTimeout = requestTimeout
	streamTransport := defaultTransport.Clone()
	streamTransport.Proxy = nil
	streamTransport.MaxIdleConns = 1
	streamTransport.MaxIdleConnsPerHost = 1
	streamTransport.MaxConnsPerHost = 1
	streamTransport.ResponseHeaderTimeout = requestTimeout
	return &Client{
		baseURL:           strings.TrimSuffix(baseURL, "/"),
		http:              &http.Client{Transport: restTransport, Timeout: requestTimeout, CheckRedirect: rejectRedirect},
		streamHTTP:        &http.Client{Transport: streamTransport, CheckRedirect: rejectRedirect},
		streamIdleTimeout: streamTimeout,
		limiter:           newRateLimiter(minInterval),
		blockCache:        make(map[uint64]*blockCacheEntry),
		canonicalRoots:    make(map[uint64]*canonicalRootEntry),
		headPolls:         make(map[uint64]*headPollEntry),
		committeeCache:    make(map[uint64]*committeeCacheEntry),
	}
}

func rejectRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

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

// getEnvelope is get for endpoints whose top-level metadata is part of the
// evidence. Most Beacon API calls only need data, but header responses carry
// execution_optimistic outside data and must not silently discard it.
func (c *Client) getEnvelope(ctx context.Context, path string, out any) (found bool, err error) {
	if err := c.limiter.wait(ctx); err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return false, fmt.Errorf("build request for %s: %w", path, err)
	}
	body, found, err := c.readResponse(req)
	if err != nil || !found || out == nil {
		return found, err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("%s %s: decode response: %w", req.Method, req.URL.Path, err)
	}
	return true, nil
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
	body, found, err := c.readResponse(req)
	if err != nil || !found || out == nil {
		return found, err
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false, fmt.Errorf("%s %s: decode response: %w", req.Method, req.URL.Path, err)
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return false, fmt.Errorf("%s %s: decode data field: %w", req.Method, req.URL.Path, err)
	}
	return true, nil
}

func (c *Client) readResponse(req *http.Request) ([]byte, bool, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails

	// 410 Gone is the attestation pool endpoint's way of saying a slot's data
	// aged out of the pool (pruned, or its epoch moved on) — the window for
	// seeing something published there has closed, the same non-failure
	// meaning as a 404 on a block that was never produced.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096)) //nolint:errcheck // best-effort diagnostic snippet for the error message below; a read failure here just yields an empty snippet
		return nil, false, &httpStatusError{method: req.Method, path: req.URL.Path, statusCode: resp.StatusCode, body: string(body)}
	}
	if resp.ContentLength > maxResponseBodyBytes {
		return nil, false, fmt.Errorf("%s %s: response body is %d bytes, limit is %d", req.Method, req.URL.Path, resp.ContentLength, maxResponseBodyBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("%s %s: read response: %w", req.Method, req.URL.Path, err)
	}
	if len(body) > maxResponseBodyBytes {
		return nil, false, fmt.Errorf("%s %s: response body exceeds %d bytes", req.Method, req.URL.Path, maxResponseBodyBytes)
	}
	return body, true, nil
}

type httpStatusError struct {
	method     string
	path       string
	statusCode int
	body       string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("%s %s: unexpected status %d: %q", e.method, e.path, e.statusCode, strings.TrimSpace(e.body))
}

// rateLimiter enforces a minimum spacing between successive requests — a
// plain mutex-guarded timestamp rather than a token-bucket package, since
// I-5 only requires *a* ceiling on request rate, not a burst allowance, and
// this needs no new dependency (I-14) to say so.
type rateLimiter struct {
	minInterval time.Duration

	mu      sync.Mutex
	next    time.Time
	queue   []*rateWaiter
	running bool
}

type rateWaiter struct {
	ctx   context.Context
	ready chan struct{}
}

func newRateLimiter(minInterval time.Duration) *rateLimiter {
	return &rateLimiter{minInterval: minInterval}
}

// wait blocks in FIFO order until minInterval has elapsed since the previous
// grant, or ctx is done. FIFO matters under validator load: an inclusion scan
// must not repeatedly win the mutex and starve a deadline-sensitive block or
// attestation poll.
func (r *rateLimiter) wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w := &rateWaiter{ctx: ctx, ready: make(chan struct{})}
	r.mu.Lock()
	r.queue = append(r.queue, w)
	if !r.running {
		r.running = true
		go r.run()
	}
	r.mu.Unlock()

	select {
	case <-ctx.Done():
		select {
		case <-w.ready:
			return nil
		default:
			return ctx.Err()
		}
	case <-w.ready:
		return nil
	}
}

func (r *rateLimiter) run() {
	for {
		r.mu.Lock()
		for len(r.queue) > 0 && r.queue[0].ctx.Err() != nil {
			r.queue[0] = nil
			r.queue = r.queue[1:]
		}
		if len(r.queue) == 0 {
			r.queue = nil
			r.running = false
			r.mu.Unlock()
			return
		}
		w := r.queue[0]
		wait := time.Until(r.next)
		r.mu.Unlock()

		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-w.ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				continue
			case <-timer.C:
			}
		}

		r.mu.Lock()
		if len(r.queue) == 0 || r.queue[0] != w || w.ctx.Err() != nil {
			r.mu.Unlock()
			continue
		}
		r.queue[0] = nil
		r.queue = r.queue[1:]
		r.next = time.Now().Add(r.minInterval)
		close(w.ready)
		r.mu.Unlock()
	}
}
