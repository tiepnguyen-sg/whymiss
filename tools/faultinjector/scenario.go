package main

import (
	"fmt"
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

	// Description is one sentence: what was broken and how, for
	// test/corpus/<ID>/README.md.
	Description string `yaml:"description"`

	// Target names which devnet participant the fault is applied to, in the form
	// "<index>-<cl_type>-<el_type>" as Kurtosis names services in the enclave —
	// e.g. "el-1-geth-lighthouse" or "cl-2-prysm-geth" — discoverable via
	// `make devnet.info`.
	Target string `yaml:"target"`

	// ValidatorIndex is whose duty is being watched. The scenario targets a
	// validator known to be keyed to Target's validator client.
	ValidatorIndex uint64 `yaml:"validator_index"`

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
	// io.pressure or memory.pressure after the observation window and records
	// it as a host_sampled observation (metric "iowait_pct" or
	// "mem_pressure_pct"). Meaningful for a fault that runs on Target's own
	// container — cgroup_io/cgroup_mem most directly, since those are what
	// actually generate the corresponding stall — so this only applies when
	// Target is the container under load.
	SamplePressure string `yaml:"sample_pressure,omitempty"`

	// MetricsTarget, when set, names a Kurtosis service (normally an execution
	// client) whose Prometheus endpoint is scraped after the observation window
	// for Engine API call durations, recorded as engine_call observations. Set
	// this to the execution client actually under load — usually the same
	// participant as Target for an el_slow-focused scenario.
	MetricsTarget string `yaml:"metrics_target,omitempty"`

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
	return s, nil
}

// Validate reports why the scenario cannot be run, or nil.
func (s Scenario) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("id is required")
	}
	if s.Target == "" {
		return fmt.Errorf("target is required")
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
	return s.Fault.Validate()
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
	case "cgroup_io":
		if f.CgroupIO == nil {
			return fmt.Errorf("fault.kind is %q but cgroup_io params are not set", f.Kind)
		}
	case "cgroup_cpu":
		if f.CgroupCPU == nil {
			return fmt.Errorf("fault.kind is %q but cgroup_cpu params are not set", f.Kind)
		}
	case "cgroup_mem":
		if f.CgroupMem == nil {
			return fmt.Errorf("fault.kind is %q but cgroup_mem params are not set", f.Kind)
		}
	case "pause":
		if f.Pause == nil {
			return fmt.Errorf("fault.kind is %q but pause params are not set", f.Kind)
		}
	case "clock_skew":
		if f.ClockSkew == nil {
			return fmt.Errorf("fault.kind is %q but clock_skew params are not set", f.Kind)
		}
	case "peer_drop":
		if f.PeerDrop == nil {
			return fmt.Errorf("fault.kind is %q but peer_drop params are not set", f.Kind)
		}
	default:
		return fmt.Errorf("fault.kind %q is not one of netem/cgroup_io/cgroup_cpu/cgroup_mem/pause/clock_skew/peer_drop", f.Kind)
	}
	return nil
}
