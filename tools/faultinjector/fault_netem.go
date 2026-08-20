package main

import (
	"context"
	"fmt"
)

// NetemParams configures a network-degradation fault via `tc netem`.
type NetemParams struct {
	// Delay is added latency, e.g. "500ms".
	Delay string `yaml:"delay,omitempty"`
	// LossPercent is packet loss, 0-100.
	LossPercent float64 `yaml:"loss_percent,omitempty"`
}

// NetemFault degrades the network path to a service's container by attaching a
// netem qdisc to the host-side veth interface of its Docker network pair, rather
// than inside the container's own network namespace.
//
// # Why the host side
//
// A container's default capability set has no NET_ADMIN (verified: `tc` is not
// even installed in the release images, and adding it would still fail —
// `unshare --time` against a similarly-privileged operation in the same
// container returned "Operation not permitted"). Applying the qdisc to the
// host-side veth end instead needs no capability inside the target container at
// all — the host owns that interface — which is the standard technique chaos
// tools (Pumba and similar) use against unmodified Docker containers.
//
// # Verification status — read before trusting a scenario built with this
//
// The veth-discovery step (matching a container's eth0 to its host-side peer
// interface) was prototyped against this project's Docker-Desktop-for-Mac
// development sandbox and did not reliably reproduce a measurable delay there:
// Docker Desktop's network stack does not expose a plain Linux bridge+veth
// topology the way native Linux Docker does, so ifindex/MAC-based peer discovery
// found interfaces that did not carry the container's actual traffic. The
// mechanism is architecturally sound and used by prior art on native Linux
// (I-13's release target); it has not been confirmed effective through this
// codebase yet. Do not generate a corpus scenario with this fault until that
// confirmation happens on a real Linux host — an unconfirmed fault produces a
// scenario indistinguishable from one where nothing was actually injected,
// which is worse than not having the scenario (docs/BUILD_PROMPT.md §8).
type NetemFault struct {
	Params NetemParams
}

// Apply is intentionally unimplemented pending the verification above.
func (f *NetemFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	return nil, fmt.Errorf("netem: mechanism not yet verified against a real devnet — see the NetemFault doc comment before implementing Apply")
}
