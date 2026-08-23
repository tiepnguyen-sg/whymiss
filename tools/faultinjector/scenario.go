package main

import (
	"fmt"
	"math"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Scenario is a declarative fault-injection recipe: what to break, how, for how
// long, and what the taxonomy predicts whymiss should conclude from it.
//
// A Scenario file does not itself contain observations — those are produced by
// actually running the scenario against a live devnet (Run, in main.go) and
// written to test/corpus/<ID>/observations.jsonl. The scenario file is the
// reproducible recipe; the corpus directory is the recorded result of following
// it once.
type Scenario struct {
	// ID names the scenario and becomes its corpus directory name
	// (test/corpus/<ID>/). Lowercase, hyphen-separated, e.g. "el-disk-stall".
	ID string `yaml:"id"`

	// RecipeID is runtime-only provenance. LoadScenario sets it to the YAML
	// ID before a campaign may replace ID with a unique record ID.
	RecipeID string `yaml:"-"`

	// Description is one sentence: what was broken and how, for
	// test/corpus/<ID>/README.md.
	Description string `yaml:"description"`

	// Target names which devnet participant the fault is applied to, in the form
	// "<index>-<cl_type>-<el_type>" as Kurtosis names services in the enclave —
	// e.g. "el-1-geth-lighthouse" or "cl-2-prysm-geth" — discoverable via
	// `make devnet.info`.
	Target string `yaml:"target"`

	// ValidatorIndex is whose duty is being watched. The scenario targets a
	// validator known to be keyed to Target's validator client. Ignored when
	// ValidatorCandidates is set.
	ValidatorIndex uint64 `yaml:"validator_index"`

	// ValidatorCandidates, when set, is an inclusive [min, max] range of
	// validator indices any one of which is an acceptable subject — normally
	// the whole validator set keyed to Target's validator client, since a
	// fault applied to that client affects all of them identically.
	//
	// This exists because attester duty is assigned once per epoch per
	// validator: with a single fixed ValidatorIndex there is exactly one
	// candidate slot per epoch, so a scenario that also constrains the
	// proposer (RequireProposerValidators) only has a ~50% chance of a
	// usable slot on this two-node devnet and fails outright otherwise,
	// leaving "run it again next epoch" as the only recourse (observed: five
	// consecutive misses). Given a range, findCleanDuty asks for every
	// candidate's duty in one request and picks whichever one lands on a
	// slot that satisfies the constraint, which makes a usable slot
	// essentially certain on the first attempt.
	//
	// The validator actually chosen is what gets recorded in the manifest
	// and observations, so a scenario generated this way is still a
	// concrete, reproducible record of one real validator's duty.
	ValidatorCandidates *[2]uint64 `yaml:"validator_candidates,omitempty"`

	// Fault names which mechanism to apply and its parameters. Exactly one of
	// the typed fields on Fault is set, matching Fault.Kind.
	Fault FaultSpec `yaml:"fault"`

	// Duration is how long the fault is held before being reverted.
	Duration time.Duration `yaml:"duration"`

	// AvoidProposerValidators, when set, excludes slots whose proposer duty
	// falls in [min, max] (inclusive) from being chosen as the watched slot.
	// Set this to the fault's own target's validator range when the fault
	// affects every validator on a node (e.g. pause): otherwise a slot where
	// that same node also happens to hold the proposer duty confounds the
	// scenario with network.proposer_missed, which is not what the scenario is
	// meant to isolate. Verified necessary in practice — an initial run of
	// vc-frozen-lighthouse without this picked exactly such a slot.
	AvoidProposerValidators *[2]uint64 `yaml:"avoid_proposer_validators,omitempty"`

	// RequireProposerValidators, when set, restricts the watched slot to one
	// whose proposer duty falls in [min, max] (inclusive) — the opposite of
	// AvoidProposerValidators. Set this to the fault target's own validator
	// range when the fault is meant to delay block *production* itself (e.g.
	// throttling the proposer's node so the block is genuinely late for every
	// observer, not just this one) rather than the watched validator's own
	// attestation path. Mutually exclusive with AvoidProposerValidators — a
	// scenario isolates one confound or requires the other, never both.
	RequireProposerValidators *[2]uint64 `yaml:"require_proposer_validators,omitempty"`

	// SamplePressure, when set to "io" or "memory", reads Target's cgroup v2
	// io.pressure or memory.pressure during the faulted duty slot and records
	// it as a host_sampled observation (metric "host_iowait_pct" or
	// "host_mem_pressure_pct"). Meaningful for a fault that runs on Target's own
	// container — cgroup_io/cgroup_mem most directly, since those are what
	// actually generate the corresponding stall — so this only applies when
	// Target is the container under load.
	SamplePressure string `yaml:"sample_pressure,omitempty"`

	// TimingTarget is the consensus client whose block-arrival gauge is
	// sampled when the watched slot reaches head.
	TimingTarget string `yaml:"timing_target"`

	// BaselineTarget is an independent, unfaulted consensus client whose
	// block-arrival metric supplies the network comparison for R-110/R-200.
	BaselineTarget string `yaml:"baseline_target,omitempty"`

	// SampleEngineCalls records exact per-slot Engine durations when both
	// required cumulative counters advance exactly once.
	SampleEngineCalls bool `yaml:"sample_engine_calls,omitempty"`

	// PeerCountTarget, when set, names a Kurtosis consensus-client service
	// (e.g. "cl-1-lighthouse-geth") whose peer count is scraped after the
	// observation window and recorded as a peer_count_sampled observation.
	// A separate field from Target (mirroring MetricsTarget's shape) since
	// the duty being watched may live on the VC while the CL whose peering
	// matters is a different service name.
	PeerCountTarget string `yaml:"peer_count_target,omitempty"`

	// Expect is what docs/causes.md predicts this fault produces. corpusctl
	// checks it against the taxonomy; it is not checked against the engine here
	// — internal/rca does not exist until Phase 3. Recording the prediction now
	// is what makes the scenario a labelled example rather than just a log.
	Expect Expectation `yaml:"expect"`
}

// Expectation is the label a corpus scenario carries: what a correct verdict
// should say, per docs/causes.md.
type Expectation struct {
	Cause      string `yaml:"cause"`
	SubCause   string `yaml:"sub_cause,omitempty"`
	Confidence string `yaml:"confidence"`
}

const maxScenarioValidatorCandidates = 64

// FaultSpec names one fault mechanism and its parameters. Exactly one field
// besides Kind is populated, matching Kind's value — enforced by Validate.
type FaultSpec struct {
	// Kind selects the mechanism: "netem", "cgroup_io", "cgroup_cpu",
	// "cgroup_mem", "pause", "clock_skew", or "peer_drop". See fault_*.go for
	// the implementation of each.
	Kind string `yaml:"kind"`

	Netem     *NetemParams     `yaml:"netem,omitempty"`
	CgroupIO  *CgroupIOParams  `yaml:"cgroup_io,omitempty"`
	CgroupCPU *CgroupCPUParams `yaml:"cgroup_cpu,omitempty"`
	CgroupMem *CgroupMemParams `yaml:"cgroup_mem,omitempty"`
	Pause     *PauseParams     `yaml:"pause,omitempty"`
	ClockSkew *ClockSkewParams `yaml:"clock_skew,omitempty"`
	PeerDrop  *PeerDropParams  `yaml:"peer_drop,omitempty"`
}

// LoadScenario reads and validates a scenario file.
func LoadScenario(path string) (Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenario %s: %w", path, err)
	}

	var s Scenario
	if err := yaml.Unmarshal(data, &s); err != nil {
		return Scenario{}, fmt.Errorf("parse scenario %s: %w", path, err)
	}
	if err := s.Validate(); err != nil {
		return Scenario{}, fmt.Errorf("scenario %s: %w", path, err)
	}
	s.RecipeID = s.ID
	return s, nil
}

// Validate reports why the scenario cannot be run, or nil.
func (s Scenario) Validate() error {
	if !validScenarioID(s.ID) {
		return fmt.Errorf("id %q must contain only lowercase letters, digits, and internal hyphens", s.ID)
	}
	if s.Target == "" {
		return fmt.Errorf("target is required")
	}
	if s.TimingTarget == "" {
		return fmt.Errorf("timing_target is required")
	}
	if (s.Expect.Cause == "local.p2p_degraded" || s.Expect.Cause == "network.late_block") && s.BaselineTarget == "" {
		return fmt.Errorf("baseline_target is required for %s", s.Expect.Cause)
	}
	if s.Expect.Cause == "local.p2p_degraded" {
		if s.Fault.Netem == nil || s.Fault.Netem.PeerTarget == "" {
			return fmt.Errorf("fault.netem.peer_target is required for local.p2p_degraded so observability traffic is not faulted")
		}
		if s.Fault.Netem.PeerTarget == s.Target {
			return fmt.Errorf("fault.netem.peer_target must differ from the fault target")
		}
	}
	if s.Duration <= 0 {
		return fmt.Errorf("duration must be positive, got %s", s.Duration)
	}
	if s.Expect.Cause == "" {
		return fmt.Errorf("expect.cause is required — an unlabelled scenario is not a corpus scenario")
	}
	if s.Expect.Confidence == "" {
		return fmt.Errorf("expect.confidence is required")
	}
	if s.SamplePressure != "" && s.SamplePressure != "io" && s.SamplePressure != "memory" {
		return fmt.Errorf("sample_pressure must be \"io\" or \"memory\", got %q", s.SamplePressure)
	}
	if s.AvoidProposerValidators != nil && s.RequireProposerValidators != nil {
		return fmt.Errorf("avoid_proposer_validators and require_proposer_validators are mutually exclusive")
	}
	if s.ValidatorCandidates != nil && s.ValidatorCandidates[0] > s.ValidatorCandidates[1] {
		return fmt.Errorf("validator_candidates range %v is inverted", *s.ValidatorCandidates)
	}
	if s.ValidatorCandidates != nil && s.ValidatorCandidates[1]-s.ValidatorCandidates[0] >= maxScenarioValidatorCandidates {
		return fmt.Errorf("validator_candidates range %v exceeds %d validators", *s.ValidatorCandidates, maxScenarioValidatorCandidates)
	}
	if s.Fault.PeerDrop != nil && s.Fault.PeerDrop.PeerTarget == s.Target {
		return fmt.Errorf("fault.peer_drop.peer_target must differ from the fault target")
	}
	return s.Fault.Validate()
}

func validScenarioID(id string) bool {
	if id == "" || id[0] == '-' || id[len(id)-1] == '-' {
		return false
	}
	for i := range len(id) {
		c := id[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
		if c == '-' && i > 0 && id[i-1] == '-' {
			return false
		}
	}
	return true
}

// Validate reports why the fault spec is malformed, or nil.
func (f FaultSpec) Validate() error {
	set := 0
	for _, present := range []bool{
		f.Netem != nil, f.CgroupIO != nil, f.CgroupCPU != nil, f.CgroupMem != nil,
		f.Pause != nil, f.ClockSkew != nil, f.PeerDrop != nil,
	} {
		if present {
			set++
		}
	}
	if set != 1 {
		return fmt.Errorf("fault: exactly one of netem/cgroup_io/cgroup_cpu/cgroup_mem/pause/clock_skew/peer_drop must be set, got %d", set)
	}

	switch f.Kind {
	case "netem":
		if f.Netem == nil {
			return fmt.Errorf("fault.kind is %q but netem params are not set", f.Kind)
		}
		if f.Netem.Delay != "" {
			delay, err := time.ParseDuration(f.Netem.Delay)
			if err != nil || delay <= 0 {
				return fmt.Errorf("fault.netem.delay must be a positive duration, got %q", f.Netem.Delay)
			}
		}
		if math.IsNaN(f.Netem.LossPercent) || math.IsInf(f.Netem.LossPercent, 0) || f.Netem.LossPercent < 0 || f.Netem.LossPercent > 100 {
			return fmt.Errorf("fault.netem.loss_percent must be finite and between 0 and 100, got %v", f.Netem.LossPercent)
		}
		if f.Netem.Delay == "" && f.Netem.LossPercent == 0 {
			return fmt.Errorf("fault.netem requires delay or positive loss_percent")
		}
	case "cgroup_io":
		if f.CgroupIO == nil {
			return fmt.Errorf("fault.kind is %q but cgroup_io params are not set", f.Kind)
		}
		if f.CgroupIO.ReadBytesPerSec == 0 && f.CgroupIO.WriteBytesPerSec == 0 {
			return fmt.Errorf("fault.cgroup_io requires read_bytes_per_sec or write_bytes_per_sec")
		}
	case "cgroup_cpu":
		if f.CgroupCPU == nil {
			return fmt.Errorf("fault.kind is %q but cgroup_cpu params are not set", f.Kind)
		}
		if f.CgroupCPU.QuotaPercent == 0 || f.CgroupCPU.QuotaPercent > 100 {
			return fmt.Errorf("fault.cgroup_cpu.quota_percent must be between 1 and 100, got %d", f.CgroupCPU.QuotaPercent)
		}
	case "cgroup_mem":
		if f.CgroupMem == nil {
			return fmt.Errorf("fault.kind is %q but cgroup_mem params are not set", f.Kind)
		}
		if f.CgroupMem.LimitBytes == 0 {
			return fmt.Errorf("fault.cgroup_mem.limit_bytes must be positive")
		}
	case "pause":
		if f.Pause == nil {
			return fmt.Errorf("fault.kind is %q but pause params are not set", f.Kind)
		}
	case "clock_skew":
		if f.ClockSkew == nil {
			return fmt.Errorf("fault.kind is %q but clock_skew params are not set", f.Kind)
		}
		offset, err := time.ParseDuration(f.ClockSkew.Offset)
		if err != nil || offset == 0 {
			return fmt.Errorf("fault.clock_skew.offset must be a non-zero duration, got %q", f.ClockSkew.Offset)
		}
	case "peer_drop":
		if f.PeerDrop == nil {
			return fmt.Errorf("fault.kind is %q but peer_drop params are not set", f.Kind)
		}
		if f.PeerDrop.PeerTarget == "" {
			return fmt.Errorf("fault.peer_drop.peer_target is required")
		}
	default:
		return fmt.Errorf("fault.kind %q is not one of netem/cgroup_io/cgroup_cpu/cgroup_mem/pause/clock_skew/peer_drop", f.Kind)
	}
	return nil
}
