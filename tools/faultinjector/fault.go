package main

import (
	"context"
	"fmt"
)

// Fault applies one mechanism to a running devnet service and reverts it. Every
// implementation lives in its own fault_*.go file, one mechanism per file — the
// same discipline internal/rca/rules will apply to rules (docs/BUILD_PROMPT.md
// §7).
type Fault interface {
	// Apply injects the fault against target and returns a function that reverts
	// it. Apply must not block for the fault's Duration itself — the caller
	// (Run, in main.go) owns timing, so every fault is held for exactly the
	// duration the scenario declares and reverted the same way regardless of
	// which mechanism was used.
	Apply(ctx context.Context, enclave, target string) (revert func(context.Context) error, err error)
}

// NewFault constructs the Fault named by spec.Kind. Returns an error for a Kind
// FaultSpec.Validate should already have rejected — defensive, since Run calls
// this after validating, not a path a well-formed scenario file can reach.
func NewFault(spec FaultSpec) (Fault, error) {
	switch spec.Kind {
	case "netem":
		return &NetemFault{Params: *spec.Netem}, nil
	case "cgroup_io":
		return &CgroupIOFault{Params: *spec.CgroupIO}, nil
	case "pause":
		return &PauseFault{Params: *spec.Pause}, nil
	case "clock_skew":
		return &ClockSkewFault{Params: *spec.ClockSkew}, nil
	case "peer_drop":
		return &PeerDropFault{Params: *spec.PeerDrop}, nil
	default:
		return nil, fmt.Errorf("unknown fault kind %q", spec.Kind)
	}
}
