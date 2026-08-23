package promscrape

import (
	"context"
	"testing"
	"time"
)

func TestSampleLighthouseEngineCounters(t *testing.T) {
	t.Parallel()
	server := serveTestdata(t, "lighthouse_metrics.txt")
	t.Cleanup(server.Close)
	got, err := New().SampleLighthouseEngineCounters(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.NewPayload.Count != 2 || abs(got.NewPayload.SumMS-11.881873) > 1e-6 {
		t.Fatalf("newPayload = %+v, want count 2 sum 11.881873ms", got.NewPayload)
	}
	if got.ForkchoiceUpdated.Count != 5 || abs(got.ForkchoiceUpdated.SumMS-23.614143) > 1e-6 {
		t.Fatalf("forkchoiceUpdated = %+v, want count 5 sum 23.614143ms", got.ForkchoiceUpdated)
	}
}

func TestSamplePrysmEngineCounters(t *testing.T) {
	t.Parallel()
	server := serveTestdata(t, "prysm_metrics.txt")
	t.Cleanup(server.Close)
	got, err := New().SamplePrysmEngineCounters(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.NewPayload.Count != 0 || got.NewPayload.SumMS != 0 {
		t.Fatalf("newPayload = %+v, want zero", got.NewPayload)
	}
	if got.ForkchoiceUpdated.Count != 1 || got.ForkchoiceUpdated.SumMS != 1 {
		t.Fatalf("forkchoiceUpdated = %+v, want count 1 sum 1ms", got.ForkchoiceUpdated)
	}
}

func TestEngineCallsBetween(t *testing.T) {
	before := EngineCounters{
		SampledAt:         time.Unix(10, 0),
		NewPayload:        EngineCounter{Count: 8, SumMS: 100},
		ForkchoiceUpdated: EngineCounter{Count: 12, SumMS: 50},
	}
	after := EngineCounters{
		SampledAt:         time.Unix(11, 0),
		NewPayload:        EngineCounter{Count: 9, SumMS: 112.5},
		ForkchoiceUpdated: EngineCounter{Count: 13, SumMS: 53},
	}
	calls, err := EngineCallsBetween(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Method != EngineMethodNewPayload || calls[0].Count != 1 || calls[0].DurationMS != 12.5 || calls[1].Method != EngineMethodForkchoiceUpdated || calls[1].Count != 1 || calls[1].DurationMS != 3 {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestEngineCallsBetweenAllowsMultipleCallsPerMethod(t *testing.T) {
	before := EngineCounters{SampledAt: time.Unix(10, 0)}
	after := EngineCounters{
		SampledAt:         time.Unix(11, 0),
		NewPayload:        EngineCounter{Count: 2, SumMS: 12},
		ForkchoiceUpdated: EngineCounter{Count: 3, SumMS: 9},
	}
	calls, err := EngineCallsBetween(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if calls[0].Count != 2 || calls[1].Count != 3 {
		t.Fatalf("calls = %+v, want counts 2 and 3", calls)
	}
}

func TestEngineCallsBetweenRejectsMissingMethod(t *testing.T) {
	before := EngineCounters{SampledAt: time.Unix(10, 0)}
	after := EngineCounters{
		SampledAt:         time.Unix(11, 0),
		NewPayload:        EngineCounter{Count: 1, SumMS: 12},
		ForkchoiceUpdated: EngineCounter{},
	}
	if _, err := EngineCallsBetween(before, after); err == nil {
		t.Fatal("EngineCallsBetween error = nil, want missing-method error")
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
