package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/source"
)

type failingEngineSampler struct{ calls int }

func (f *failingEngineSampler) SampleEngineCounters(context.Context, source.ConsensusClient, string) (source.EngineCounters, error) {
	f.calls++
	return source.EngineCounters{}, fmt.Errorf("metrics endpoint unavailable")
}

// TestCollectEngineBaselineGivesUp pins the bound. A node whose Engine counters
// are absent, reset every slot, or served by a client this build has no adapter
// for would otherwise spin one slot at a time until the context died, and a
// corpus run would hang instead of failing with something an operator can act
// on — which is what every other polling loop in this project already avoids.
func TestCollectEngineBaselineGivesUp(t *testing.T) {
	t.Parallel()
	sampler := &failingEngineSampler{}
	// A one-millisecond slot keeps the test fast; the loop's bound is counted in
	// attempts, not wall clock.
	obs := &Observer{SecondsPerSlot: time.Millisecond}

	_, err := collectEngineBaseline(context.Background(), sampler, source.ConsensusLighthouse, "http://unused/metrics", obs)
	if err == nil {
		t.Fatal("collectEngineBaseline returned no error against a sampler that never succeeds")
	}
	for _, want := range []string{"engine baseline collected 0 of", "not reporting usable Engine counters"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if sampler.calls < source.EngineBaselineMinSamples {
		t.Errorf("gave up after %d scrapes, want it to try at least %d before concluding", sampler.calls, source.EngineBaselineMinSamples)
	}
	if sampler.calls > source.EngineBaselineMinSamples*3 {
		t.Errorf("made %d scrapes, want the bound to stop it by %d", sampler.calls, source.EngineBaselineMinSamples*3)
	}
}

func TestCollectEngineBaselineStopsOnCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := collectEngineBaseline(ctx, &failingEngineSampler{}, source.ConsensusLighthouse, "http://unused/metrics", &Observer{SecondsPerSlot: time.Millisecond}); err == nil {
		t.Fatal("collectEngineBaseline ignored a cancelled context")
	}
}
