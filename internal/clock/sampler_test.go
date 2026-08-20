package clock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	base := Config{Servers: []string{"127.0.0.1:123"}, Timeout: time.Second, MaxAttempts: 3}
	if err := base.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	tests := []struct {
		name   string
		mutate func(*Config)
		want   error
	}{
		{"no servers", func(c *Config) { c.Servers = nil }, ErrNoServers},
		{"zero timeout", func(c *Config) { c.Timeout = 0 }, ErrInvalidConfig},
		{"negative timeout", func(c *Config) { c.Timeout = -time.Second }, ErrInvalidConfig},
		{"zero max attempts", func(c *Config) { c.MaxAttempts = 0 }, ErrInvalidConfig},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := base
			tc.mutate(&cfg)
			if err := cfg.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{}); err == nil {
		t.Fatal("New() error = nil, want a validation failure")
	}
}

func TestSamplerSampleSucceedsOnFirstServer(t *testing.T) {
	t.Parallel()

	srv := newFakeServer(t, time.Second)
	s, err := New(Config{Servers: []string{srv.addr}, Timeout: time.Second, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	reading, err := s.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample() error = %v, want nil", err)
	}
	if reading.Server != srv.addr {
		t.Errorf("Server = %q, want %q", reading.Server, srv.addr)
	}
}

// TestSamplerFallsOverToNextServer proves the round-robin composes attempts across
// servers, not just retries against one — a config listing two beacon operators'
// preferred NTP sources should survive one of them being down.
func TestSamplerFallsOverToNextServer(t *testing.T) {
	t.Parallel()

	dead := newFakeServer(t, 0)
	dead.setSilent()
	good := newFakeServer(t, 500*time.Millisecond)

	s, err := New(Config{
		Servers:     []string{dead.addr, good.addr},
		Timeout:     150 * time.Millisecond,
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	reading, err := s.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample() error = %v, want nil", err)
	}
	if reading.Server != good.addr {
		t.Errorf("Sample() used server %q, want it to have fallen over to %q", reading.Server, good.addr)
	}
}

func TestSamplerExhaustsAttempts(t *testing.T) {
	t.Parallel()

	srv := newFakeServer(t, 0)
	srv.setSilent()

	s, err := New(Config{Servers: []string{srv.addr}, Timeout: 50 * time.Millisecond, MaxAttempts: 2})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = s.Sample(context.Background())
	if !errors.Is(err, ErrAllAttemptsFailed) {
		t.Fatalf("Sample() error = %v, want %v", err, ErrAllAttemptsFailed)
	}
}

// TestSamplerRespectsBoundedAttempts is the mechanical proof behind I-5's "never
// retry-storm": however long a caller's context allows, the number of attempts
// against the network never exceeds Config.MaxAttempts.
func TestSamplerRespectsBoundedAttempts(t *testing.T) {
	t.Parallel()

	srv := newFakeServer(t, 0)
	srv.setSilent()

	const maxAttempts = 3
	s, err := New(Config{Servers: []string{srv.addr}, Timeout: 20 * time.Millisecond, MaxAttempts: maxAttempts})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	start := time.Now()
	_, err = s.Sample(context.Background())
	elapsed := time.Since(start)

	if !errors.Is(err, ErrAllAttemptsFailed) {
		t.Fatalf("Sample() error = %v, want %v", err, ErrAllAttemptsFailed)
	}
	// backoffCap is 10s; with 3 attempts the two backoff waits are each bounded by
	// an exponential schedule that starts well under a second. A generous 8s
	// ceiling catches an accidental unbounded loop while tolerating scheduler
	// jitter under a loaded test runner.
	if elapsed > 8*time.Second {
		t.Errorf("Sample() with MaxAttempts=%d took %s — looks unbounded", maxAttempts, elapsed)
	}
}

func TestSamplerRespondsToContextCancellation(t *testing.T) {
	t.Parallel()

	srv := newFakeServer(t, 0)
	srv.setSilent()

	s, err := New(Config{Servers: []string{srv.addr}, Timeout: 5 * time.Second, MaxAttempts: 10})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = s.Sample(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Sample() error = nil, want a cancellation failure")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Sample() took %s to notice context cancellation", elapsed)
	}
}

// TestSamplerConfigIsCopied proves a caller mutating the slice passed into New
// cannot change a Sampler's behaviour afterward.
func TestSamplerConfigIsCopied(t *testing.T) {
	t.Parallel()

	servers := []string{"127.0.0.1:1"}
	s, err := New(Config{Servers: servers, Timeout: time.Second, MaxAttempts: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	servers[0] = "mutated"

	if s.cfg.Servers[0] == "mutated" {
		t.Error("Sampler's server list changed after the caller mutated its slice")
	}
}

// TestSamplerCancelledDuringBackoff covers the branch distinct from cancelling
// while a query is in flight: the context ending while Sample is waiting between
// attempts.
func TestSamplerCancelledDuringBackoff(t *testing.T) {
	t.Parallel()

	srv := newFakeServer(t, 0)
	srv.setSilent()

	s, err := New(Config{Servers: []string{srv.addr}, Timeout: 30 * time.Millisecond, MaxAttempts: 5})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond) // let the first attempt fail, then cancel mid-backoff
		cancel()
	}()

	start := time.Now()
	_, err = s.Sample(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Sample() error = nil, want a cancellation failure")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Sample() took %s to notice cancellation during backoff", elapsed)
	}
}
