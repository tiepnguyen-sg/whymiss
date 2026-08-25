//go:build darwin

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNetemFaultAgainstDockerDesktopDevnet(t *testing.T) {
	if os.Getenv("WHYMISS_NETEM_INTEGRATION") == "" {
		t.Skip("set WHYMISS_NETEM_INTEGRATION=1 to run against Docker Desktop")
	}
	target := os.Getenv("WHYMISS_NETEM_TARGET_CONTAINER")
	peer := os.Getenv("WHYMISS_NETEM_PEER_CONTAINER")
	beaconURL := os.Getenv("WHYMISS_NETEM_TARGET_URL")
	if target == "" || peer == "" || beaconURL == "" {
		t.Fatal("WHYMISS_NETEM_TARGET_CONTAINER, WHYMISS_NETEM_PEER_CONTAINER, and WHYMISS_NETEM_TARGET_URL are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	targetID, err := dockerContainerID(ctx, "", target)
	if err != nil {
		t.Fatal(err)
	}
	targetIP, err := dockerContainerIPv4(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	peerID, err := dockerContainerID(ctx, "", peer)
	if err != nil {
		t.Fatal(err)
	}
	baselineAPI := beaconRequestDuration(t, ctx, beaconURL)
	baselinePeer := dockerDesktopPeerPingRTT(t, ctx, peerID, targetIP)
	fault := NetemFault{Params: NetemParams{Delay: "300ms", PeerTarget: peer}}
	revert, err := fault.Apply(ctx, "", target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := revert(cleanupCtx); err != nil {
			t.Errorf("revert: %v", err)
		}
	}()
	delayedPeer := dockerDesktopPeerPingRTT(t, ctx, peerID, targetIP)
	if delayedPeer-baselinePeer < 200*time.Millisecond {
		t.Fatalf("peer RTT grew by %s (baseline %s, delayed %s), want at least 200ms", delayedPeer-baselinePeer, baselinePeer, delayedPeer)
	}
	delayedAPI := beaconRequestDuration(t, ctx, beaconURL)
	if delayedAPI-baselineAPI >= 200*time.Millisecond {
		t.Fatalf("Beacon API latency grew by %s (baseline %s, delayed %s); scoped netem leaked into observability traffic", delayedAPI-baselineAPI, baselineAPI, delayedAPI)
	}
}

func dockerDesktopPeerPingRTT(t *testing.T, ctx context.Context, peerID, targetIP string) time.Duration {
	t.Helper()
	pidOut, err := exec.CommandContext(ctx, "docker", "inspect", peerID, "--format", "{{.State.Pid}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	pid := strings.TrimSpace(string(pidOut))
	args := dockerDesktopHelperArgs("nsenter", "-t", pid, "-n", "--", "ping", "-c", "3", "-W", "2", targetIP)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ping %s from peer %s: %v\n%s", targetIP, peerID, err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "min/avg/max") {
			continue
		}
		_, values, ok := strings.Cut(line, "=")
		if !ok {
			break
		}
		parts := strings.Split(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(values), "ms")), "/")
		if len(parts) < 2 {
			break
		}
		avgMS, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			break
		}
		return time.Duration(avgMS * float64(time.Millisecond))
	}
	t.Fatal(fmt.Errorf("parse ping RTT from %q", string(out)))
	return 0
}

func TestCgroupFaultAgainstDockerDesktopDevnet(t *testing.T) {
	if os.Getenv("WHYMISS_CGROUP_INTEGRATION") == "" {
		t.Skip("set WHYMISS_CGROUP_INTEGRATION=1 to run against Docker Desktop")
	}
	target := os.Getenv("WHYMISS_CGROUP_TARGET_CONTAINER")
	if target == "" {
		t.Fatal("WHYMISS_CGROUP_TARGET_CONTAINER is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	id, err := dockerContainerID(ctx, "", target)
	if err != nil {
		t.Fatal(err)
	}
	original, err := readContainerCgroupFile(ctx, id, "cpu.max")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := writeContainerCgroupFile(cleanupCtx, id, "cpu.max", strings.TrimSpace(string(original))); err != nil {
			t.Errorf("restore cpu.max: %v", err)
		}
	}()
	applyCtx, cancelApply := context.WithCancel(ctx)
	fault := CgroupCPUFault{Params: CgroupCPUParams{QuotaPercent: 50}}
	revert, err := fault.Apply(applyCtx, "", target)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readContainerCgroupFile(ctx, id, "cpu.max")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "50000 100000" {
		t.Fatalf("cpu.max = %q", strings.TrimSpace(string(got)))
	}
	// The scenario context is exactly what cancellation invalidates. Revert must
	// honor the fresh cleanup context RunScenario supplies instead of retaining
	// the canceled Apply context.
	cancelApply()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()
	if err := revert(cleanupCtx); err != nil {
		t.Fatalf("revert with fresh context after Apply cancellation: %v", err)
	}
	restored, err := readContainerCgroupFile(cleanupCtx, id, "cpu.max")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(restored)) != strings.TrimSpace(string(original)) {
		t.Fatalf("restored cpu.max = %q, want %q", strings.TrimSpace(string(restored)), strings.TrimSpace(string(original)))
	}
}

func TestCgroupMemoryPressureAgainstDockerDesktopDevnet(t *testing.T) {
	if os.Getenv("WHYMISS_CGROUP_MEM_INTEGRATION") == "" {
		t.Skip("set WHYMISS_CGROUP_MEM_INTEGRATION=1 to run against Docker Desktop")
	}
	target := os.Getenv("WHYMISS_CGROUP_MEM_TARGET_CONTAINER")
	if target == "" {
		t.Fatal("WHYMISS_CGROUP_MEM_TARGET_CONTAINER is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	id, err := dockerContainerID(ctx, "", target)
	if err != nil {
		t.Fatal(err)
	}
	original, err := readContainerCgroupFile(ctx, id, "memory.high")
	if err != nil {
		t.Fatal(err)
	}
	fault := CgroupMemFault{Params: CgroupMemParams{LimitBytes: 64 << 20, PressureBytes: 256 << 20}}
	revert, err := fault.Apply(ctx, "", target)
	if err != nil {
		t.Fatal(err)
	}
	reverted := false
	defer func() {
		if reverted {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := revert(cleanupCtx); err != nil {
			t.Errorf("revert memory.high: %v", err)
		}
	}()

	timer := time.NewTimer(16 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	pressure, err := SampleMemoryPressure(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if pressure <= 10 {
		t.Fatalf("memory.pressure some avg10 = %.2f%%, want >10%%", pressure)
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()
	if err := revert(cleanupCtx); err != nil {
		t.Fatal(err)
	}
	reverted = true
	restored, err := readContainerCgroupFile(cleanupCtx, id, "memory.high")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(restored)) != strings.TrimSpace(string(original)) {
		t.Fatalf("restored memory.high = %q, want %q", strings.TrimSpace(string(restored)), strings.TrimSpace(string(original)))
	}
}

func beaconRequestDuration(t *testing.T, ctx context.Context, beaconURL string) time.Duration {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, beaconURL+"/eth/v1/node/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("health status = %d", resp.StatusCode)
	}
	return time.Since(started)
}
