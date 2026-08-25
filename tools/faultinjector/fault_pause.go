package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// PauseParams configures a container-freeze fault.
type PauseParams struct{}

// PauseFault freezes a service's container entirely: `docker pause` suspends
// every process in it via the kernel freezer cgroup, with no CPU scheduling at
// all, until `docker unpause` resumes it exactly where it left off.
//
// This is the fault behind local.vc_disconnected and, applied to a CL service,
// stands in for a wedged consensus client process — from the beacon API's
// perspective indistinguishable from a hang, which is the point.
//
// Verified against a live Kurtosis devnet (test/e2e/kurtosis): pausing and
// unpausing a running service's container round-trips cleanly through plain
// `docker pause`/`docker unpause`, no elevated privileges needed beyond what
// invoking the Docker CLI already requires.
type PauseFault struct {
	Params PauseParams
}

// Apply resolves target's container and pauses it. Docker's own container-name
// resolution needs no help from Kurtosis for this — see dockerContainerID.
func (f *PauseFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	id, err := dockerContainerID(ctx, enclave, target)
	if err != nil {
		return nil, err
	}

	if out, err := exec.CommandContext(ctx, "docker", "pause", id).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker pause %s: %w\n%s", id, err, out)
	}

	revert := func(ctx context.Context) error {
		if out, err := exec.CommandContext(ctx, "docker", "unpause", id).CombinedOutput(); err != nil {
			return fmt.Errorf("docker unpause %s: %w\n%s", id, err, out)
		}
		return nil
	}
	return revert, nil
}

// dockerContainerID resolves a Kurtosis service name to the underlying Docker
// container ID. Kurtosis names containers "<service-name>--<service-uuid>", so a
// name-prefix filter is enough — no Kurtosis SDK, no parsing `kurtosis service
// inspect` output, just the Docker CLI whose behavior this whole tool otherwise
// stays out of the way of.
//
// enclave additionally scopes the search to one Kurtosis enclave via the
// kurtosis_enclave_name container label. Every ethereum-package devnet uses
// the same service names (cl-1-lighthouse-geth, vc-2-geth-prysm, ...), so two
// enclaves running side by side (parallel corpus generation) each have a
// same-named container and a bare name-prefix filter matches both —
// discovered when running two devnets at once made every fault kind fail
// with "matched 2 containers, want exactly 1" before any fault was applied
// (fail-closed, not fail-dangerous: Apply never ran against the wrong
// enclave's container). enclave == "" skips the label filter, preserving the
// original single-enclave behavior for callers and tests that predate
// parallel generation and have no enclave name to pass.
//
// This ties container-level faults (pause, cgroup_io) to the Docker backend
// specifically. Fine for now: BUILD_PROMPT names no other backend, and
// docs/BUILD_PROMPT.md task 1.5 targets "a Kurtosis devnet" without specifying
// Kubernetes. If that changes, this is the one function that needs a Kubernetes
// equivalent — everything above it stays the same.
func dockerContainerID(ctx context.Context, enclave, serviceName string) (string, error) {
	args := []string{
		"ps",
		"--filter", "name=" + serviceName + "--",
		"--no-trunc",
		"--format", "{{.ID}}",
	}
	if enclave != "" {
		args = append(args, "--filter", "label=kurtosis_enclave_name="+enclave)
	}
	//nolint:gosec // G702: serviceName and enclave are operator-supplied
	// Kurtosis names (CLI flag or test/corpus manifest field), never
	// attacker/network input, and are passed as argv elements to
	// exec.CommandContext — no shell is involved, so there is no injection
	// vector for gosec's taint analysis to actually be flagging here.
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return "", fmt.Errorf("docker ps --filter name=%s--: %w", serviceName, err)
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return "", fmt.Errorf("no running container found for service %q (enclave %q)", serviceName, enclave)
	}
	if len(ids) > 1 {
		return "", fmt.Errorf("service %q matched %d containers, want exactly 1 (pass a non-empty enclave to disambiguate multiple running enclaves): %v", serviceName, len(ids), ids)
	}
	return ids[0], nil
}
