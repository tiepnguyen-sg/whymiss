package rca_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
	"github.com/tiepnguyen-sg/whymiss/internal/timeline"
)

// TestAnalyze_Deterministic re-analyzes the same real timeline 1000 times
// and asserts byte-identical JSON output — Analyze is pure (ADR-0003, I-6),
// so repeated calls on the same input must never diverge (task 3.9).
func TestAnalyze_Deterministic(t *testing.T) {
	dir := filepath.Join("..", "..", "test", "corpus", "cl-slow-cpu")

	obs, err := timeline.LoadObservations(filepath.Join(dir, "observations.jsonl"))
	if err != nil {
		t.Fatalf("LoadObservations: %v", err)
	}
	tl, err := timeline.Replay(obs, domain.MainnetPreEPBS())
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	cfg := rca.DefaultConfig()
	first := rca.Analyze(tl, cfg)
	want, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	for i := 0; i < 1000; i++ {
		v := rca.Analyze(tl, cfg)
		got, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal (iteration %d): %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("iteration %d: Analyze produced different JSON:\nfirst: %s\ngot:   %s", i, want, got)
		}
	}
}
