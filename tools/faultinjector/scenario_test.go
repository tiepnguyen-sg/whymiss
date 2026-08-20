package main

import (
	"testing"
	"time"
)

func validScenario() Scenario {
	return Scenario{
		ID: "test-scenario", Description: "d", Target: "vc-1-geth-lighthouse",
		ValidatorIndex: 1, Duration: 10 * time.Second,
		Fault:  FaultSpec{Kind: "pause", Pause: &PauseParams{}},
		Expect: Expectation{Cause: "local.vc_disconnected", Confidence: "high"},
	}
}

func TestScenarioValidate(t *testing.T) {
	t.Parallel()

	if err := validScenario().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	tests := []struct {
		name   string
		mutate func(*Scenario)
	}{
		{"no id", func(s *Scenario) { s.ID = "" }},
		{"no target", func(s *Scenario) { s.Target = "" }},
		{"zero duration", func(s *Scenario) { s.Duration = 0 }},
		{"negative duration", func(s *Scenario) { s.Duration = -time.Second }},
		{"no expected cause", func(s *Scenario) { s.Expect.Cause = "" }},
		{"no expected confidence", func(s *Scenario) { s.Expect.Confidence = "" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := validScenario()
			tc.mutate(&s)
			if err := s.Validate(); err == nil {
				t.Error("Validate() = nil, want an error")
			}
		})
	}
}

func TestFaultSpecValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    FaultSpec
		wantErr bool
	}{
		{"pause, matching params", FaultSpec{Kind: "pause", Pause: &PauseParams{}}, false},
		{"netem, matching params", FaultSpec{Kind: "netem", Netem: &NetemParams{Delay: "1s"}}, false},
		{"cgroup_io, matching params", FaultSpec{Kind: "cgroup_io", CgroupIO: &CgroupIOParams{}}, false},
		{"clock_skew, matching params", FaultSpec{Kind: "clock_skew", ClockSkew: &ClockSkewParams{Offset: "1s"}}, false},
		{"peer_drop, matching params", FaultSpec{Kind: "peer_drop", PeerDrop: &PeerDropParams{}}, false},
		{"no params at all", FaultSpec{Kind: "pause"}, true},
		{"kind/params mismatch", FaultSpec{Kind: "pause", Netem: &NetemParams{}}, true},
		{"two params set at once", FaultSpec{Kind: "pause", Pause: &PauseParams{}, Netem: &NetemParams{}}, true},
		{"unknown kind", FaultSpec{Kind: "meteor_strike", Pause: &PauseParams{}}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.spec.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewFault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind    string
		spec    FaultSpec
		wantErr bool
	}{
		{"pause", FaultSpec{Kind: "pause", Pause: &PauseParams{}}, false},
		{"netem", FaultSpec{Kind: "netem", Netem: &NetemParams{}}, false},
		{"cgroup_io", FaultSpec{Kind: "cgroup_io", CgroupIO: &CgroupIOParams{}}, false},
		{"clock_skew", FaultSpec{Kind: "clock_skew", ClockSkew: &ClockSkewParams{}}, false},
		{"peer_drop", FaultSpec{Kind: "peer_drop", PeerDrop: &PeerDropParams{}}, false},
		{"unknown", FaultSpec{Kind: "unknown"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()

			f, err := NewFault(tc.spec)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewFault() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && f == nil {
				t.Error("NewFault() returned nil Fault with nil error")
			}
		})
	}
}

func TestLoadScenarioRejectsMissingFile(t *testing.T) {
	t.Parallel()

	if _, err := LoadScenario("does/not/exist.yaml"); err == nil {
		t.Error("LoadScenario() error = nil, want a file-not-found error")
	}
}
