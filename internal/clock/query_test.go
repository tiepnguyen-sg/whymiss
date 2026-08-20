package clock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQuerySuccess(t *testing.T) {
	t.Parallel()

	const skew = 3 * time.Second
	srv := newFakeServer(t, skew)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reading, err := query(ctx, srv.addr)
	if err != nil {
		t.Fatalf("query() error = %v, want nil", err)
	}

	// The fake server's clock runs `skew` ahead of ours, so the measured offset
	// should land close to it. Loopback round trip is sub-millisecond; a
	// generous 200ms tolerance keeps this from flaking under test-runner load
	// while still catching a formula that is wrong by anything meaningful.
	diff := reading.Offset - skew
	if diff < 0 {
		diff = -diff
	}
	if diff > 200*time.Millisecond {
		t.Errorf("query() offset = %s, want close to %s (diff %s)", reading.Offset, skew, diff)
	}
	if reading.Server != srv.addr {
		t.Errorf("Server = %q, want %q", reading.Server, srv.addr)
	}
	if reading.RoundTrip < 0 {
		t.Errorf("RoundTrip = %s, want non-negative", reading.RoundTrip)
	}
	if reading.At.Location() != time.UTC {
		t.Errorf("At.Location() = %v, want UTC", reading.At.Location())
	}
}

func TestQueryTimeout(t *testing.T) {
	t.Parallel()

	srv := newFakeServer(t, 0)
	srv.setSilent()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := query(ctx, srv.addr)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("query() error = nil, want a timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("query() error = %v, want context.DeadlineExceeded in the chain", err)
	}
	if elapsed > time.Second {
		t.Errorf("query() took %s to time out against a 100ms deadline", elapsed)
	}
}

func TestQueryRejectsBadResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		break_ func(*fakeServer)
	}{
		{"stratum 0 kiss-of-death", func(s *fakeServer) { s.setStratum(0) }},
		{"leap indicator unsynchronized", func(s *fakeServer) { s.setLeapUnsync() }},
		{"truncated reply", func(s *fakeServer) { s.setShortReply(20) }},
		{"wrong mode in reply", func(s *fakeServer) { s.setMode(modeClient) }},
		{"origin timestamp does not match what was sent", func(s *fakeServer) { s.setWrongOrigin() }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := newFakeServer(t, 0)
			tc.break_(srv)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			_, err := query(ctx, srv.addr)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("query() error = %v, want %v", err, ErrInvalidResponse)
			}
		})
	}
}

func TestQueryUnreachableAddress(t *testing.T) {
	t.Parallel()

	// A reserved TEST-NET-1 address (RFC 5737) with nothing listening: the OS
	// should refuse the connection quickly rather than this test needing a real
	// network timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := query(ctx, "192.0.2.1:123"); err == nil {
		t.Fatal("query() error = nil, want a failure against an unreachable address")
	}
}
