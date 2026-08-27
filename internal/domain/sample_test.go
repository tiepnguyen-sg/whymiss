package domain_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestComponentValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		c    domain.Component
		want bool
	}{
		{domain.ComponentEL, true},
		{domain.ComponentCL, true},
		{domain.ComponentVC, true},
		{domain.ComponentHost, true},
		{"relay", false},
		{"", false},
	}

	for _, tc := range tests {
		if got := tc.c.Valid(); got != tc.want {
			t.Errorf("Component(%q).Valid() = %v, want %v", tc.c, got, tc.want)
		}
	}
}

func validSample() domain.MetricSample {
	return domain.MetricSample{
		At: at, Component: domain.ComponentEL, Name: "engine_newpayload_ms",
		Value: 240, Source: domain.SourcePromScrape,
	}
}

func TestMetricSampleValidate(t *testing.T) {
	t.Parallel()

	if err := validSample().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	tests := []struct {
		name   string
		mutate func(*domain.MetricSample)
	}{
		{"empty name", func(s *domain.MetricSample) { s.Name = "" }},
		{"oversized name", func(s *domain.MetricSample) { s.Name = domain.MetricName(strings.Repeat("x", 129)) }},
		{"unknown component", func(s *domain.MetricSample) { s.Component = "relay" }},
		{"zero timestamp", func(s *domain.MetricSample) { s.At = time.Time{} }},
		{"not utc", func(s *domain.MetricSample) { s.At = at.In(time.FixedZone("CET", 3600)) }},
		{"unattributed", func(s *domain.MetricSample) { s.Source = "" }},
		{"known source on wrong component", func(s *domain.MetricSample) { s.Source = domain.SourceHostMetrics }},
		{"beacon API on a non-CL component", func(s *domain.MetricSample) { s.Source = domain.SourceBeaconAPI; s.Component = domain.ComponentHost }},
		{"nan value", func(s *domain.MetricSample) { s.Value = math.NaN() }},
		{"infinite value", func(s *domain.MetricSample) { s.Value = math.Inf(1) }},
		{"measured without sample time", func(s *domain.MetricSample) { s.ClockMeasured = true }},
		{"unmeasured with offset", func(s *domain.MetricSample) { s.ClockOffset = time.Millisecond }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := validSample()
			tc.mutate(&s)
			if err := s.Validate(); err == nil {
				t.Error("Validate() = nil, want an error")
			}
		})
	}
}

func validBaseline() domain.NetworkBaseline {
	return domain.NetworkBaseline{
		Slot: 100, BlockArrivalP50: time.Second, BlockArrivalP90: 2 * time.Second,
		SampleCount: 40, Source: domain.SourceXatu,
	}
}

func TestNetworkBaselineValidate(t *testing.T) {
	t.Parallel()

	if err := validBaseline().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	tests := []struct {
		name   string
		mutate func(*domain.NetworkBaseline)
	}{
		{"zero sample count", func(b *domain.NetworkBaseline) { b.SampleCount = 0 }},
		{"negative sample count", func(b *domain.NetworkBaseline) { b.SampleCount = -1 }},
		{"negative p50", func(b *domain.NetworkBaseline) { b.BlockArrivalP50 = -time.Millisecond }},
		{"p50 exceeds p90", func(b *domain.NetworkBaseline) { b.BlockArrivalP50, b.BlockArrivalP90 = 3*time.Second, time.Second }},
		{"unattributed", func(b *domain.NetworkBaseline) { b.Source = "" }},
		{"known source that cannot measure a baseline", func(b *domain.NetworkBaseline) { b.Source = domain.SourceHostMetrics }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b := validBaseline()
			tc.mutate(&b)
			if err := b.Validate(); err == nil {
				t.Error("Validate() = nil, want an error")
			}
		})
	}
}

func TestNetworkBaselineFromObservation(t *testing.T) {
	obs, err := domain.NewObservation(domain.Observation{
		Slot: 100, Kind: domain.ObsNetworkBaselineSampled, At: at, Source: domain.SourceXatu,
		Attrs: map[domain.AttrKey]string{
			domain.AttrBlockArrivalP50MS: "850.5",
			domain.AttrBlockArrivalP90MS: "1300",
			domain.AttrSampleCount:       "42",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := domain.NetworkBaselineFromObservation(obs)
	if err != nil {
		t.Fatalf("NetworkBaselineFromObservation: %v", err)
	}
	if got.BlockArrivalP50 != 850500*time.Microsecond || got.BlockArrivalP90 != 1300*time.Millisecond || got.SampleCount != 42 {
		t.Fatalf("baseline = %+v", got)
	}
}

func TestNetworkBaselineFromObservationRejectsMalformedValues(t *testing.T) {
	// Construct the public value directly to keep the decoder defensive even
	// though NewObservation rejects this malformed wire value earlier.
	obs := domain.Observation{
		Slot: 100, Kind: domain.ObsNetworkBaselineSampled, At: at, Source: domain.SourceXatu,
		Attrs: map[domain.AttrKey]string{
			domain.AttrBlockArrivalP50MS: "NaN",
			domain.AttrBlockArrivalP90MS: "1300",
			domain.AttrSampleCount:       "42",
		},
	}
	if _, err := domain.NetworkBaselineFromObservation(obs); err == nil {
		t.Fatal("NetworkBaselineFromObservation accepted NaN")
	}
}
