package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// realGethMetricsSample is a trimmed excerpt of an actual response from
// geth's /debug/metrics/prometheus endpoint, captured against this project's
// devnet — see docs/BUILD_PROMPT.md §8: never hand-write a mock response.
const realGethMetricsSample = `# TYPE engine_getblobs_available gauge
engine_getblobs_available 0
# TYPE rpc_duration_engine_exchangeCapabilities_success_count counter
rpc_duration_engine_exchangeCapabilities_success_count 1
# TYPE rpc_duration_engine_exchangeCapabilities_success summary
rpc_duration_engine_exchangeCapabilities_success {quantile="0.5"} 189720
rpc_duration_engine_exchangeCapabilities_success {quantile="0.75"} 189720
# TYPE rpc_duration_engine_forkchoiceUpdatedV3_success_count counter
rpc_duration_engine_forkchoiceUpdatedV3_success_count 2
# TYPE rpc_duration_engine_forkchoiceUpdatedV3_success summary
rpc_duration_engine_forkchoiceUpdatedV3_success {quantile="0.5"} 2.22248e+06
rpc_duration_engine_forkchoiceUpdatedV3_success {quantile="0.75"} 3.79379e+06
rpc_duration_engine_forkchoiceUpdatedV3_success {quantile="0.95"} 3.79379e+06
# TYPE rpc_duration_engine_newPayloadV4_success_count counter
rpc_duration_engine_newPayloadV4_success_count 1
# TYPE rpc_duration_engine_newPayloadV4_success summary
rpc_duration_engine_newPayloadV4_success {quantile="0.5"} 2.12666e+06
rpc_duration_engine_newPayloadV4_success {quantile="0.75"} 2.12666e+06
`

func TestSampleEngineCallDurations(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(realGethMetricsSample))
	}))
	defer srv.Close()

	samples, err := SampleEngineCallDurations(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("SampleEngineCallDurations() error = %v", err)
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i].Method < samples[j].Method })

	want := []EngineCallSample{
		{Method: "forkchoiceUpdated", DurationMS: 2.22248},
		{Method: "newPayload", DurationMS: 2.12666},
	}
	if len(samples) != len(want) {
		t.Fatalf("got %d samples, want %d: %+v", len(samples), len(want), samples)
	}
	for i, w := range want {
		if samples[i].Method != w.Method {
			t.Errorf("samples[%d].Method = %q, want %q", i, samples[i].Method, w.Method)
		}
		if diff := samples[i].DurationMS - w.DurationMS; diff > 1e-6 || diff < -1e-6 {
			t.Errorf("samples[%d].DurationMS = %v, want %v", i, samples[i].DurationMS, w.DurationMS)
		}
	}
}

func TestSampleEngineCallDurationsIgnoresUnrelatedMetrics(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# TYPE engine_getblobs_available gauge\nengine_getblobs_available 0\n"))
	}))
	defer srv.Close()

	samples, err := SampleEngineCallDurations(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("SampleEngineCallDurations() error = %v", err)
	}
	if len(samples) != 0 {
		t.Errorf("SampleEngineCallDurations() = %+v, want none", samples)
	}
}

func TestSampleEngineCallDurationsRejectsBadStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := SampleEngineCallDurations(context.Background(), srv.URL); err == nil {
		t.Error("SampleEngineCallDurations() error = nil, want an error for 404")
	}
}
