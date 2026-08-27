package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// NetemParams configures a network-degradation fault via `tc netem`.
type NetemParams struct {
	// Delay is added latency, e.g. "500ms". Passed through to `tc` verbatim.
	Delay string `yaml:"delay,omitempty"`
	// LossPercent is packet loss, 0-100.
	LossPercent float64 `yaml:"loss_percent,omitempty"`
	// PeerTargets optionally scopes the fault to packets arriving from these
	// Kurtosis services. This keeps Beacon API, metrics, and Engine traffic
	// observable while degrading only the P2P path under test.
	//
	// It is a list, not a single peer, because the devnet is a mesh. Scoping a
	// p2p fault to one peer's IP degrades nothing measurable once a third node
	// can relay the same gossip around the throttled link — which is exactly
	// what happened when the devnet gained its third participant. Naming every
	// peer whose gossip the scenario means to degrade is the only version of
	// this fault that survives a topology change.
	PeerTargets []string `yaml:"peer_targets,omitempty"`
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
// Confirmed on native Linux and Docker Desktop: attaching `netem delay` to the
// resolved host-side veth measurably adds the requested latency and reverts to
// baseline. Native Linux requires root (or CAP_SYS_ADMIN + CAP_NET_ADMIN).
// Docker Desktop uses a short-lived privileged helper inside its Linux VM.
// These privileges belong only to this development tool, never the shipped
// whymiss binary.
type NetemFault struct {
	Params NetemParams
}

// Apply resolves target's host-side veth and attaches the netem qdisc.
func (f *NetemFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	id, err := dockerContainerID(ctx, enclave, target)
	if err != nil {
		return nil, err
	}
	peerIPs, err := netemPeerIPs(ctx, enclave, f.Params.PeerTargets)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "darwin" {
		loss := ""
		if f.Params.LossPercent > 0 {
			loss = strconv.FormatFloat(f.Params.LossPercent, 'f', -1, 64) + "%"
		}
		if err := runDockerDesktopNetem(ctx, id, "add", f.Params.Delay, loss, strings.Join(peerIPs, " ")); err != nil {
			return nil, err
		}
		return func(ctx context.Context) error {
			return runDockerDesktopNetem(ctx, id, "del", "", "", "")
		}, nil
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

	revert := func(ctx context.Context) error {
		out, err := exec.CommandContext(ctx, "tc", "qdisc", "del", "dev", veth, "root").CombinedOutput()
		if err != nil {
			return fmt.Errorf("tc qdisc del dev %s root: %w\n%s", veth, err, out)
		}
		return nil
	}
	if len(peerIPs) == 0 {
		if err := runTC(ctx, args...); err != nil {
			return nil, err
		}
		return revert, nil
	}

	prioArgs := []string{
		"qdisc", "add", "dev", veth, "root", "handle", "1:", "prio", "bands", "4", "priomap",
		"0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0",
	}
	if err := runTC(ctx, prioArgs...); err != nil {
		return nil, err
	}
	cleanupOnError := func(applyErr error) (func(context.Context) error, error) {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if cleanupErr := revert(cleanupCtx); cleanupErr != nil {
			return nil, errors.Join(applyErr, fmt.Errorf("cleanup scoped netem: %w", cleanupErr))
		}
		return nil, applyErr
	}
	netemArgs := []string{"qdisc", "add", "dev", veth, "parent", "1:4", "handle", "40:", "netem"}
	if f.Params.Delay != "" {
		netemArgs = append(netemArgs, "delay", f.Params.Delay)
	}
	if f.Params.LossPercent > 0 {
		netemArgs = append(netemArgs, "loss", strconv.FormatFloat(f.Params.LossPercent, 'f', -1, 64)+"%")
	}
	if err := runTC(ctx, netemArgs...); err != nil {
		return cleanupOnError(err)
	}
	// One filter per peer, all steering into the same throttled band.
	for _, peerIP := range peerIPs {
		filterArgs := []string{"filter", "add", "dev", veth, "protocol", "ip", "parent", "1:", "prio", "1", "u32", "match", "ip", "src", peerIP + "/32", "flowid", "1:4"}
		if err := runTC(ctx, filterArgs...); err != nil {
			return cleanupOnError(err)
		}
	}
	return revert, nil
}

func runTC(ctx context.Context, args ...string) error {
	if out, err := exec.CommandContext(ctx, "tc", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("tc %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

func netemPeerIPs(ctx context.Context, enclave string, peerTargets []string) ([]string, error) {
	ips := make([]string, 0, len(peerTargets))
	for _, peerTarget := range peerTargets {
		if peerTarget == "" {
			continue
		}
		id, err := dockerContainerID(ctx, enclave, peerTarget)
		if err != nil {
			return nil, fmt.Errorf("resolve netem peer target %s: %w", peerTarget, err)
		}
		ip, err := dockerContainerIPv4(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("resolve netem peer IP for %s: %w", peerTarget, err)
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

func dockerContainerIPv4(ctx context.Context, containerID string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "inspect", containerID,
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}").Output()
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(out))
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return "", fmt.Errorf("invalid IPv4 address %q", ip)
	}
	return ip, nil
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
