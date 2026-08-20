package clock

import (
	"context"
	"fmt"
	"time"
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
	if c.Timeout <= 0 {
		return fmt.Errorf("%w: timeout must be positive, got %s", ErrInvalidConfig, c.Timeout)
	}
	if c.MaxAttempts < 1 {
		return fmt.Errorf("%w: max_attempts must be at least 1, got %d", ErrInvalidConfig, c.MaxAttempts)
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
	copy(servers, cfg.Servers)
	cfg.Servers = servers
	return &Sampler{cfg: cfg}, nil
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
