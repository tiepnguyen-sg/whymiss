package main

import (
	"context"
	"fmt"
)

// ClockSkewParams configures a fault that skews a client's perceived wall clock.
type ClockSkewParams struct {
	// Offset is the skew to apply, e.g. "+2s" or "-500ms" — parsed with
	// time.ParseDuration after stripping an optional leading sign.
	Offset string `yaml:"offset"`
}

// ClockSkewFault is meant to skew a running client's clock via libfaketime
// (LD_PRELOAD) reading a live-updatable FAKETIME_TIMESTAMP_FILE — the standard way
// to change an already-running process's apparent time without restarting it,
// since LD_PRELOAD itself only takes effect at process exec.
//
// # Why this is not implemented yet
//
// That requires the target process to have been *launched* with
// LD_PRELOAD=libfaketime.so and FAKETIME_TIMESTAMP_FILE set — neither of which
// the release CL images carry, and neither of which a fault can retroactively
// attach to an already-running process (verified: unshare --time against a
// running container's process also fails with "Operation not permitted", and
// even with the capability, a namespace unshare only affects processes forked
// afterward, not the one already running). Making this fault real means
// changing test/e2e/kurtosis/network_params.yaml so the target participant's
// cl_extra_env_vars/el_extra_env_vars preload libfaketime from genesis, which is
// a devnet-configuration change, not something this fault applies at run time —
// scoped out of this pass rather than implemented against an unverified
// assumption about what the images contain (docs/BUILD_PROMPT.md §8).
type ClockSkewFault struct {
	Params ClockSkewParams
}

// Apply is intentionally unimplemented pending the devnet configuration change
// described above.
func (f *ClockSkewFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	return nil, fmt.Errorf("clock_skew: requires target participant launched with libfaketime preloaded — see the ClockSkewFault doc comment")
}
