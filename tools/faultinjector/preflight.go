package main

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// consensusServiceName reads a consensus node's service name out of one line of
// `kurtosis enclave inspect` output, and reports false for every other row —
// execution clients, validator clients, and added services all appear there too.
//
// Spelled out as a field check rather than a package-level regexp because this
// repository permits no mutable package-level state (make check.globals), and
// every other parser here states its rules the same way; validScenarioID is the
// nearest example.
func consensusServiceName(line string) (string, bool) {
	if !strings.Contains(line, "RUNNING") {
		return "", false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", false
	}
	// Consensus services are named cl-<n>-<client>-<el>, so the digit is what
	// separates them from el- and vc- rows sharing the prefix shape.
	rest, ok := strings.CutPrefix(fields[1], "cl-")
	if !ok || rest == "" || rest[0] < '0' || rest[0] > '9' {
		return "", false
	}
	return fields[1], true
}

// requiredPeers is how many peers each consensus node must report for the mesh
// to be whole: every other consensus node in the enclave.
//
// This replaced a fixed `minPreflightPeers = 1`, correct for the two-node devnet
// it was written for and silently wrong once a third participant was added for
// network.late_block. On a three-node mesh a node reporting one peer has lost
// half its view, and the consequence is not local: the missing node's validators
// propose nothing, so every recipe's block_skipped observations are polluted
// whether or not the recipe names that node. A test pinned the old constant at
// 1, so the stale assumption was guarded rather than caught.
func requiredPeers(nodes int) int { return nodes - 1 }

// preflightPeering refuses to record a scenario unless every consensus node the
// run depends on is actually peered.
//
// This exists because of what happened without it. A devnet left up for days of
// fault injection lost peering between its two consensus nodes at some point
// between 2026-08-25T08:11Z and 08:30Z, and nothing noticed: both nodes still
// reported is_syncing=false, sync_distance=0, and a head that advanced, because
// each kept building on its own fork from its own validators. Every scenario
// recorded after that saw no block for its duty slot and wrote a block_skipped
// observation — not because any injected fault caused a proposer to miss, but
// because the watched node could no longer see the other node's blocks at all.
// Fourteen records were generated that way and had to be deleted from the
// corpus once the pattern was spotted: every record before the break observed a
// block proposed by the *other* node, and every record after it observed no
// block at all.
//
// ADR-0015's skipped-slot proof cannot catch this on its own. It asks whether
// the node is fully synced, execution-valid, and past the slot — all of which an
// isolated node answers truthfully about its own fork. Peer count is the fact
// that distinguishes "the network skipped this slot" from "this node is alone",
// so it is checked here, before a recording exists to be trusted.
func preflightPeering(ctx context.Context, enclave string) error {
	nodes, err := consensusServices(ctx, enclave)
	if err != nil {
		return fmt.Errorf("preflight: list consensus nodes in enclave %s: %w", enclave, err)
	}
	if len(nodes) < 2 {
		return fmt.Errorf(
			"preflight: enclave %s has %d consensus node(s); a record needs a block proposed by a node other than the one being watched, which fewer than two cannot supply",
			enclave, len(nodes))
	}
	want := requiredPeers(len(nodes))
	for _, node := range nodes {
		sample, err := SamplePeerCount(ctx, enclave, node)
		if err != nil {
			return fmt.Errorf("preflight: read connected peers for %s: %w", node, err)
		}
		if sample.Value < float64(want) {
			return fmt.Errorf(
				"preflight: %s reports %.0f of the %d peers a whole mesh requires — a node cut off from part of the network still reports itself synced and advances its own fork, so slots the missing validators would have proposed get recorded as skipped; recreate the devnet (make devnet.down && make devnet.up) before generating corpus records",
				node, sample.Value, want)
		}
		fmt.Printf("faultinjector: preflight ok, %s has %.0f of %d peer(s)\n", node, sample.Value, want)
	}
	return nil
}

// consensusServices lists the enclave's running consensus nodes, so preflight
// holds the whole mesh to account instead of only the nodes a recipe happens to
// name. Checking the recipe's own targets alone is how three records were
// generated on 2026-08-26 while cl-1-lighthouse-geth sat at zero peers: their
// recipes named cl-2 and cl-3, both of which still reported a peer, so nothing
// objected even though a third of the validator set was off the network and 37%
// of slots were being skipped.
func consensusServices(ctx context.Context, enclave string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "kurtosis", "enclave", "inspect", enclave).Output()
	if err != nil {
		return nil, fmt.Errorf("kurtosis enclave inspect %s: %w", enclave, err)
	}
	var nodes []string
	for _, line := range strings.Split(string(out), "\n") {
		if name, ok := consensusServiceName(line); ok {
			nodes = append(nodes, name)
		}
	}
	sort.Strings(nodes)
	return nodes, nil
}
