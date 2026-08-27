package source

import "testing"

func TestEngineBaselineNeedsEnoughSamples(t *testing.T) {
	t.Parallel()
	var b EngineBaseline
	for i := range EngineBaselineMinSamples - 1 {
		b.Add(float64(i))
	}
	if _, ok := b.P99(); ok {
		t.Errorf("P99 returned a value from %d samples, want it to refuse below %d", b.Len(), EngineBaselineMinSamples)
	}
	b.Add(1)
	if _, ok := b.P99(); !ok {
		t.Errorf("P99 refused at exactly %d samples", EngineBaselineMinSamples)
	}
}

// TestEngineBaselineIsBounded pins I-12's bound and the eviction order: the
// window keeps the most recent EngineBaselineMaxSamples and drops the oldest.
func TestEngineBaselineIsBounded(t *testing.T) {
	t.Parallel()
	var b EngineBaseline
	for i := range EngineBaselineMaxSamples + 10 {
		b.Add(float64(i))
	}
	if b.Len() != EngineBaselineMaxSamples {
		t.Fatalf("Len = %d, want %d", b.Len(), EngineBaselineMaxSamples)
	}
	// Values 0..9 were evicted, so the window holds 10..265 and its p99 is the
	// 254th of 256 sorted values.
	got, ok := b.P99()
	if !ok {
		t.Fatal("P99 refused on a full window")
	}
	if want := float64(10 + 253); got != want {
		t.Errorf("P99 = %v, want %v — the oldest samples were not the ones dropped", got, want)
	}
}

func TestEngineBaselineIgnoresUnusableValues(t *testing.T) {
	t.Parallel()
	var b EngineBaseline
	b.Add(-1)
	if b.Len() != 0 {
		t.Errorf("Len = %d after a negative total, want 0", b.Len())
	}
}
