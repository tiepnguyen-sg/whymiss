package beaconapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestClientBoundsConnections(t *testing.T) {
	c := NewClient("http://127.0.0.1:5052", 0)
	transport, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("REST transport type = %T, want *http.Transport", c.http.Transport)
	}
	if transport.MaxConnsPerHost != maxRESTConnections || transport.MaxIdleConnsPerHost != maxRESTConnections {
		t.Fatalf("REST connection bounds = %d/%d, want %d/%d", transport.MaxConnsPerHost, transport.MaxIdleConnsPerHost, maxRESTConnections, maxRESTConnections)
	}
	if transport.Proxy != nil {
		t.Fatal("REST transport honors ambient proxy settings; only explicitly configured egress is permitted")
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	var out map[string]any
	_, err := NewClient(source.URL, 0).get(context.Background(), "/redirect", &out)
	requireContains(t, err, "unexpected status 302")
	if redirected {
		t.Fatal("beacon client followed a redirect outside the configured endpoint")
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(maxResponseBodyBytes+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var out map[string]any
	_, err := NewClient(srv.URL, 0).get(context.Background(), "/oversized", &out)
	requireContains(t, err, "limit")
}
