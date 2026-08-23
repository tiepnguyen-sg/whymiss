package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validScenario() Scenario {
	return Scenario{
		ID: "test-scenario", Description: "d", Target: "vc-1-geth-lighthouse", TimingTarget: "cl-1-lighthouse-geth",
		ValidatorIndex: 1, Duration: 10 * time.Second,
		Fault:  FaultSpec{Kind: "pause", Pause: &PauseParams{}},
		Expect: Expectation{Cause: "local.vc_disconnected", Confidence: "high"},
	}
}

func TestAllCommittedScenariosLoad(t *testing.T) {
	paths, err := filepath.Glob("scenarios/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, path := range paths {
		if strings.HasPrefix(filepath.Base(path), "._") {
			continue
		}
		checked++
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := LoadScenario(path); err != nil {
				t.Fatalf("LoadScenario(%s): %v", path, err)
			}
		})
	}
	if checked == 0 {
		t.Fatal("no committed scenarios found")
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
		{"no timing target", func(s *Scenario) { s.TimingTarget = "" }},
		{"zero duration", func(s *Scenario) { s.Duration = 0 }},
		{"negative duration", func(s *Scenario) { s.Duration = -time.Second }},
		{"no expected cause", func(s *Scenario) { s.Expect.Cause = "" }},
		{"no expected confidence", func(s *Scenario) { s.Expect.Confidence = "" }},
		{"too many validator candidates", func(s *Scenario) { s.ValidatorCandidates = &[2]uint64{0, 64} }},
		{"inverted validator candidates", func(s *Scenario) { s.ValidatorCandidates = &[2]uint64{2, 1} }},
		{"peer drop targets itself", func(s *Scenario) {
			s.Fault = FaultSpec{Kind: "peer_drop", PeerDrop: &PeerDropParams{PeerTarget: s.Target}}
		}},
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

func TestWatchedValidatorsHandlesMaxUint64(t *testing.T) {
	t.Parallel()

	rangeAtLimit := [2]uint64{^uint64(0) - 1, ^uint64(0)}
	got := watchedValidators(Scenario{ValidatorCandidates: &rangeAtLimit})
	if len(got) != 2 || got[0] != rangeAtLimit[0] || got[1] != rangeAtLimit[1] {
		t.Fatalf("watchedValidators() = %v, want %v", got, rangeAtLimit)
	}
}

func TestValidScenarioID(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"p2p-degraded", "vc-frozen-2", "r01"} {
		if !validScenarioID(id) {
			t.Errorf("validScenarioID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "-leading", "trailing-", "UPPER", "path/name", "two--parts"} {
		if validScenarioID(id) {
			t.Errorf("validScenarioID(%q) = true", id)
		}
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
		{"netem, no effect", FaultSpec{Kind: "netem", Netem: &NetemParams{}}, true},
		{"netem, invalid delay", FaultSpec{Kind: "netem", Netem: &NetemParams{Delay: "later"}}, true},
		{"netem, excessive loss", FaultSpec{Kind: "netem", Netem: &NetemParams{LossPercent: 101}}, true},
		{"cgroup_io, matching params", FaultSpec{Kind: "cgroup_io", CgroupIO: &CgroupIOParams{ReadBytesPerSec: 1}}, false},
		{"cgroup_io, no limit", FaultSpec{Kind: "cgroup_io", CgroupIO: &CgroupIOParams{}}, true},
		{"cgroup_cpu, matching params", FaultSpec{Kind: "cgroup_cpu", CgroupCPU: &CgroupCPUParams{QuotaPercent: 50}}, false},
		{"cgroup_cpu, zero quota", FaultSpec{Kind: "cgroup_cpu", CgroupCPU: &CgroupCPUParams{}}, true},
		{"cgroup_cpu, excessive quota", FaultSpec{Kind: "cgroup_cpu", CgroupCPU: &CgroupCPUParams{QuotaPercent: 101}}, true},
		{"cgroup_mem, matching params", FaultSpec{Kind: "cgroup_mem", CgroupMem: &CgroupMemParams{LimitBytes: 64 << 20}}, false},
		{"cgroup_mem, zero limit", FaultSpec{Kind: "cgroup_mem", CgroupMem: &CgroupMemParams{}}, true},
		{"clock_skew, matching params", FaultSpec{Kind: "clock_skew", ClockSkew: &ClockSkewParams{Offset: "1s"}}, false},
		{"clock_skew, zero offset", FaultSpec{Kind: "clock_skew", ClockSkew: &ClockSkewParams{}}, true},
		{"peer_drop, matching params", FaultSpec{Kind: "peer_drop", PeerDrop: &PeerDropParams{PeerTarget: "peer"}}, false},
		{"peer_drop, no target", FaultSpec{Kind: "peer_drop", PeerDrop: &PeerDropParams{}}, true},
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

func TestFaultRequiresRoot(t *testing.T) {
	for _, kind := range []string{"netem", "cgroup_io", "cgroup_cpu", "cgroup_mem"} {
		if !faultRequiresRoot(kind) {
			t.Errorf("faultRequiresRoot(%q) = false, want true", kind)
		}
	}
	for _, kind := range []string{"pause", "peer_drop", "clock_skew"} {
		if faultRequiresRoot(kind) {
			t.Errorf("faultRequiresRoot(%q) = true, want false", kind)
		}
	}
}
