package main

import "context"

// PeerDropParams configures a peer-isolation fault.
type PeerDropParams struct {
	// PeerTarget is the Kurtosis service name of the peer to drop — a CL
	// service, e.g. "cl-2-prysm-geth". This scenario's own Target field names
	// whose duty is being watched; PeerTarget names who they lose.
	PeerTarget string `yaml:"peer_target"`
}

// PeerDropFault isolates a validator from a specific peer by pausing that peer's
// container — reusing [PauseFault]'s verified mechanism, aimed at the other
// participant instead of the one whose duty is being watched.
//
// # Why not a network-layer drop
//
// The more surgical version of this fault — block traffic to one peer's IP while
// leaving the rest of the target's networking alone — needs the same host-side
// network-namespace access [NetemFault] does, which is unverified in this
// project's development sandbox (see that type's doc comment). Pausing the peer
// entirely is coarser: the target does not just lose one peer's *traffic*, that
// peer stops attesting and proposing too, so a scenario built with this fault is
// not a pure test of P2P-layer discrimination. It is still a real, verified loss
// of connectivity to that specific peer, and — with this devnet's two
// participants — it is the whole point when Target's only peer is PeerTarget:
// docs/causes.md's local.p2p_degraded can be genuinely reproduced by it even
// though the mechanism is blunter than the name "peer drop" suggests.
type PeerDropFault struct {
	Params PeerDropParams
}

// Apply pauses PeerTarget's container.
func (f *PeerDropFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	pause := &PauseFault{}
	return pause.Apply(ctx, enclave, f.Params.PeerTarget)
}
