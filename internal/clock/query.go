package clock

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// A slower exchange cannot establish a precise 100ms clock-trust decision: path
// asymmetry can dominate the measured offset. Treat it as no reading.
const maxTrustedRoundTrip = time.Second

// query performs one SNTP round trip against addr and returns the offset it
// measured.
//
// It makes exactly one attempt — no retry, no backoff. Composing attempts into a
// bounded, backed-off budget is [Sampler.Sample]'s job, so query stays a single
// well-defined unit a caller can bound with a context deadline (I-5).
//
// addr must include a port, typically ":123" — the standard NTP port.
func query(ctx context.Context, addr string) (Reading, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp", addr)
	if err != nil {
		return Reading{}, fmt.Errorf("clock: dial %s: %w", addr, err)
	}
	// A UDP socket's Close error is not actionable here: the read side is already
	// done with it by the time this runs, and there is no write buffer to flush
	// that a failed Close could mean was lost (unlike TCP). Recording it would add
	// noise without giving a caller anything to act on.
	defer func() { _ = conn.Close() }() //nolint:errcheck // Close on a UDP socket we only read from has nothing left to report

	deadline, haveDeadline := ctx.Deadline()
	if haveDeadline {
		if err := conn.SetDeadline(deadline); err != nil {
			return Reading{}, fmt.Errorf("clock: set deadline for %s: %w", addr, err)
		}
	}

	// conn.Read below only respects the fixed deadline just set — it does not
	// watch ctx.Done(), so a caller cancelling ctx for a reason other than that
	// deadline (shutdown, a sibling attempt succeeding) would otherwise block
	// until the deadline anyway. Force the read to unblock the moment ctx ends.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.SetDeadline(time.Now()) //nolint:errcheck // best-effort unblock; the Read call below reports the real failure
		case <-done:
		}
	}()

	t1 := time.Now().UTC()
	req := newRequest(t1)
	if _, err := conn.Write(req[:]); err != nil {
		return Reading{}, fmt.Errorf("clock: send to %s: %w", addr, err)
	}

	var resp packet
	n, readErr := conn.Read(resp[:])
	t4 := time.Now().UTC()
	if readErr != nil {
		return Reading{}, fmt.Errorf("clock: receive from %s: %w", addr, explainTimeout(ctx, readErr, deadline, haveDeadline))
	}
	if n < len(resp) {
		return Reading{}, fmt.Errorf("%w: %s sent %d bytes, want %d", ErrInvalidResponse, addr, n, len(resp))
	}
	if resp.leapIndicator() == leapUnsync {
		return Reading{}, fmt.Errorf("%w: %s reports its own clock unsynchronized (kiss-of-death)", ErrInvalidResponse, addr)
	}
	if resp.stratum() == 0 {
		return Reading{}, fmt.Errorf("%w: %s replied at stratum 0 (kiss-of-death)", ErrInvalidResponse, addr)
	}
	if resp.stratum() > 15 {
		return Reading{}, fmt.Errorf("%w: %s replied at invalid stratum %d", ErrInvalidResponse, addr, resp.stratum())
	}
	if resp.version() < 3 || resp.version() > 4 {
		return Reading{}, fmt.Errorf("%w: %s replied with unsupported NTP version %d", ErrInvalidResponse, addr, resp.version())
	}
	if resp.mode() != modeServer {
		return Reading{}, fmt.Errorf("%w: %s replied in mode %d, want server mode", ErrInvalidResponse, addr, resp.mode())
	}
	if resp.originTimestamp() != toNTP(t1) {
		return Reading{}, fmt.Errorf("%w: %s echoed the wrong origin timestamp", ErrInvalidResponse, addr)
	}
	if resp.receiveTimestamp() == 0 || resp.transmitTimestamp() == 0 {
		return Reading{}, fmt.Errorf("%w: %s omitted receive or transmit timestamp", ErrInvalidResponse, addr)
	}

	t2 := fromNTPNear(resp.receiveTimestamp(), t4)
	t3 := fromNTPNear(resp.transmitTimestamp(), t4)
	if t3.Before(t2) {
		return Reading{}, fmt.Errorf("%w: %s transmit timestamp precedes receive timestamp", ErrInvalidResponse, addr)
	}

	// Standard SNTP offset and round-trip-delay formulas (RFC 5905 §8):
	//   offset = ((T2 − T1) + (T3 − T4)) / 2
	//   delay  = (T4 − T1) − (T3 − T2)
	offset := (t2.Sub(t1) + t3.Sub(t4)) / 2
	delay := t4.Sub(t1) - t3.Sub(t2)
	if delay < 0 || delay > maxTrustedRoundTrip {
		return Reading{}, fmt.Errorf("%w: %s round trip %s is outside the trusted range [0,%s]", ErrInvalidResponse, addr, delay, maxTrustedRoundTrip)
	}

	return Reading{Server: addr, At: t4, Offset: offset, RoundTrip: delay}, nil
}

// explainTimeout translates a raw net.Conn error into the context error it was
// caused by, when it was caused by one.
//
// A deadline read failure surfaces from the net package as a *net.OpError wrapping
// os.ErrDeadlineExceeded, never as context.DeadlineExceeded or context.Canceled —
// so a caller doing errors.Is(err, context.DeadlineExceeded) needs help. Two cases
// forced the read to fail this way: the fixed deadline copied from ctx at the top
// of query was reached, or the watchdog goroutine cut the read short in response to
// ctx ending for another reason (cancellation, a sibling attempt succeeding).
//
// The two cases are told apart without racing against exactly when ctx's own
// internal timer fires relative to the socket's: the deadline case is decided by
// comparing wall-clock time against the deadline value query itself captured, not
// by asking ctx whether it has noticed yet. The watchdog case is decided by asking
// ctx directly, which is safe there — by the time the watchdog acts, ctx.Err() is
// already set, since closing Done() and setting Err() happen together.
func explainTimeout(ctx context.Context, readErr error, deadline time.Time, haveDeadline bool) error {
	var netErr net.Error
	isTimeout := errors.As(readErr, &netErr) && netErr.Timeout()

	if isTimeout && haveDeadline && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return readErr
}
