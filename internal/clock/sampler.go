package clock

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	maxServers       = 8
	maxServerAddress = 255
	maxAttempts      = 16
	maxTimeout       = 30 * time.Second
)

// Config configures a [Sampler]. There is no built-in default server (I-4): the
// caller supplies whatever the operator configured.
type Config struct {
	// Servers is tried in order, round-robining across the list as attempts are
	// retried. Must be non-empty.
	Servers []string

	// Timeout bounds a single attempt against a single server (I-5).
	Timeout time.Duration

	// MaxAttempts bounds the total number of attempts across all servers and
	// retries combined. This is the ceiling that keeps a string of failures from
	// becoming a retry storm (I-5): it is a count, not a duration, so it holds
	// regardless of how Timeout and the backoff shape interact.
	MaxAttempts int
}

// Validate reports why the configuration cannot be used, or nil.
func (c Config) Validate() error {
	if len(c.Servers) == 0 {
		return ErrNoServers
	}
	if len(c.Servers) > maxServers {
		return fmt.Errorf("%w: server count %d exceeds %d", ErrInvalidConfig, len(c.Servers), maxServers)
	}
	for i, server := range c.Servers {
		if strings.TrimSpace(server) == "" {
			return fmt.Errorf("%w: server %d is empty", ErrInvalidConfig, i)
		}
		if len(server) > maxServerAddress {
			return fmt.Errorf("%w: server %d address is %d bytes, limit is %d", ErrInvalidConfig, i, len(server), maxServerAddress)
		}
	}
	if c.Timeout <= 0 || c.Timeout > maxTimeout {
		return fmt.Errorf("%w: timeout must be in (0,%s], got %s", ErrInvalidConfig, maxTimeout, c.Timeout)
	}
	if c.MaxAttempts < 1 || c.MaxAttempts > maxAttempts {
		return fmt.Errorf("%w: max_attempts must be in [1,%d], got %d", ErrInvalidConfig, maxAttempts, c.MaxAttempts)
	}
	return nil
}

// Sampler measures the local clock's offset against a configured set of NTP
// servers.
//
// The zero value is not usable; construct one with [New]. A Sampler holds no
// mutable state and is safe for concurrent use.
type Sampler struct {
	cfg Config
}

// New validates cfg and returns a Sampler.
func New(cfg Config) (*Sampler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	servers := make([]string, len(cfg.Servers))
	for i, s := range cfg.Servers {
		servers[i] = withDefaultPort(strings.TrimSpace(s))
	}
	cfg.Servers = servers
	return &Sampler{cfg: cfg}, nil
}

// withDefaultPort appends the standard NTP port when addr is a bare
// hostname. Operators write servers the way every NTP client already
// accepts them ("pool.ntp.org", not "pool.ntp.org:123") — query requires a
// port, so this is where that gap gets closed, once, rather than in every
// caller.
func withDefaultPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, "123")
}

// Sample measures the offset once, trying each configured server in round-robin
// order with exponential backoff between attempts, up to Config.MaxAttempts total.
//
// It returns the first successful [Reading]. If every attempt fails, it returns
// [ErrAllAttemptsFailed] wrapping the most recent underlying error — never a
// fabricated offset. A caller that cannot get a reading should report the duty as
// unmeasurable, not guess (I-8's spirit applied to clock trust, I-9).
func (s *Sampler) Sample(ctx context.Context) (Reading, error) {
	var lastErr error
	for attempt := 0; attempt < s.cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt); err != nil {
				return Reading{}, fmt.Errorf("clock: backing off before attempt %d: %w", attempt+1, err)
			}
		}

		server := s.cfg.Servers[attempt%len(s.cfg.Servers)]
		attemptCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
		reading, err := query(attemptCtx, server)
		cancel()
		if err == nil {
			return reading, nil
		}
		lastErr = err

		if ctx.Err() != nil {
			return Reading{}, fmt.Errorf("clock: attempt %d against %s: %w", attempt+1, server, ctx.Err())
		}
	}
	return Reading{}, fmt.Errorf("%w after %d attempts: %w", ErrAllAttemptsFailed, s.cfg.MaxAttempts, lastErr)
}
