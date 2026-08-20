package domain_test

import (
	"testing"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
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
		{"unknown component", func(s *domain.MetricSample) { s.Component = "relay" }},
		{"zero timestamp", func(s *domain.MetricSample) { s.At = time.Time{} }},
		{"not utc", func(s *domain.MetricSample) { s.At = at.In(time.FixedZone("CET", 3600)) }},
		{"unattributed", func(s *domain.MetricSample) { s.Source = "" }},
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
		{"p50 exceeds p90", func(b *domain.NetworkBaseline) { b.BlockArrivalP50, b.BlockArrivalP90 = 3*time.Second, time.Second }},
		{"unattributed", func(b *domain.NetworkBaseline) { b.Source = "" }},
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
