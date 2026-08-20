package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// NetemParams configures a network-degradation fault via `tc netem`.
type NetemParams struct {
	// Delay is added latency, e.g. "500ms". Passed through to `tc` verbatim.
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
// # Verified against a real devnet
//
// Confirmed on a native Linux host (I-13's release target): pinging a target
// container measured ~0.05ms baseline, then exactly ~500ms after attaching
// `netem delay 500ms` to its host-side veth, reverting cleanly to baseline on
// removal. An earlier prototype against this project's Docker-Desktop-for-Mac
// development sandbox could not confirm this — that platform's networking is
// not a plain Linux bridge+veth topology, so veth discovery there found
// interfaces that did not carry the container's actual traffic. This
// implementation requires running faultinjector with the privileges to invoke
// `nsenter` and `tc` (root, or CAP_SYS_ADMIN + CAP_NET_ADMIN) on the host it runs
// on — expected for a fault-injection tool, unlike the shipped whymiss binary.
type NetemFault struct {
	Params NetemParams
}

// Apply resolves target's host-side veth and attaches the netem qdisc.
func (f *NetemFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	id, err := dockerContainerID(ctx, target)
	if err != nil {
		return nil, err
	}

	veth, err := hostVethFor(ctx, id)
	if err != nil {
		return nil, err
	}

	args := []string{"qdisc", "add", "dev", veth, "root", "netem"}
	if f.Params.Delay != "" {
		args = append(args, "delay", f.Params.Delay)
	}
	if f.Params.LossPercent > 0 {
		args = append(args, "loss", strconv.FormatFloat(f.Params.LossPercent, 'f', -1, 64)+"%")
	}
	if len(args) == 6 {
		return nil, fmt.Errorf("netem: neither delay nor loss_percent set")
	}

	if out, err := exec.CommandContext(ctx, "tc", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("tc %s: %w\n%s", strings.Join(args, " "), err, out)
	}

	revert := func(ctx context.Context) error {
		out, err := exec.CommandContext(ctx, "tc", "qdisc", "del", "dev", veth, "root").CombinedOutput()
		if err != nil {
			return fmt.Errorf("tc qdisc del dev %s root: %w\n%s", veth, err, out)
		}
		return nil
	}
	return revert, nil
}

// hostVethFor resolves the host-side veth interface carrying containerID's
// traffic: the container's own eth0 reports its peer ifindex in its `ip link`
// name (e.g. "eth0@if132"), and exactly one host veth reports that same ifindex
// as its own. Requires the caller to be able to enter the container's network
// namespace (root, or CAP_SYS_ADMIN) — see [NetemFault]'s doc comment.
func hostVethFor(ctx context.Context, containerID string) (string, error) {
	pidOut, err := exec.CommandContext(ctx, "docker", "inspect", containerID, "--format", "{{.State.Pid}}").Output()
	if err != nil {
		return "", fmt.Errorf("resolve pid for container %s: %w", containerID, err)
	}
	pid := strings.TrimSpace(string(pidOut))

	linkOut, err := exec.CommandContext(ctx, "nsenter", "-t", pid, "-n", "ip", "-o", "link", "show", "eth0").Output()
	if err != nil {
		return "", fmt.Errorf("inspect eth0 in container %s (pid %s): %w", containerID, pid, err)
	}
	peerIfindex, err := parsePeerIfindex(string(linkOut))
	if err != nil {
		return "", fmt.Errorf("container %s: %w", containerID, err)
	}

	candidates, err := filepath.Glob("/sys/class/net/veth*/ifindex")
	if err != nil {
		return "", fmt.Errorf("list host veth interfaces: %w", err)
	}

	var matches []string
	for _, path := range candidates {
		content, err := os.ReadFile(path) //nolint:gosec // G304: fixed glob pattern under /sys/class/net, not an operator- or attacker-supplied path
		if err != nil {
			continue // interface may have disappeared between Glob and ReadFile; not a fatal error
		}
		if strings.TrimSpace(string(content)) == peerIfindex {
			matches = append(matches, filepath.Base(filepath.Dir(path)))
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no host veth found with ifindex %s for container %s", peerIfindex, containerID)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ifindex %s matched %d host interfaces, want exactly 1: %v", peerIfindex, len(matches), matches)
	}
}

// parsePeerIfindex extracts the peer ifindex from `ip -o link show eth0`
// output, which names the interface "eth0@if<N>" when it is one half of a veth
// pair — N is the ifindex of the other half, on the host.
func parsePeerIfindex(ipLinkOutput string) (string, error) {
	const marker = "eth0@if"
	i := strings.Index(ipLinkOutput, marker)
	if i < 0 {
		return "", fmt.Errorf("eth0 is not a veth pair member (no %q marker in %q)", marker, strings.TrimSpace(ipLinkOutput))
	}
	rest := ipLinkOutput[i+len(marker):]
	end := strings.IndexAny(rest, ": \t")
	if end < 0 {
		end = len(rest)
	}
	ifindex := rest[:end]
	if ifindex == "" {
		return "", fmt.Errorf("empty peer ifindex parsed from %q", strings.TrimSpace(ipLinkOutput))
	}
	return ifindex, nil
}
