//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestNetemFaultAgainstRealDevnet is a manual integration check, not part of the
// normal test suite: it requires a running Kurtosis devnet (test/e2e/kurtosis)
// and root privileges (nsenter, tc), and is skipped unless both an explicit
// opt-in env var and a real target container are present.
//
// Run it with:
//
//	sudo WHYMISS_NETEM_INTEGRATION=1 WHYMISS_NETEM_TARGET_CONTAINER=<id> go test ./tools/faultinjector/ -run TestNetemFaultAgainstRealDevnet -v
//
// It exists to verify the Go code path (hostVethFor's parsing and matching
// logic) against a live container, distinct from the manual shell commands used
// to first confirm the mechanism works at all (see NetemFault's doc comment).
func TestNetemFaultAgainstRealDevnet(t *testing.T) {
	if os.Getenv("WHYMISS_NETEM_INTEGRATION") == "" {
		t.Skip("set WHYMISS_NETEM_INTEGRATION=1 (and run as root) to run this against a live devnet")
	}
	containerID := os.Getenv("WHYMISS_NETEM_TARGET_CONTAINER")
	if containerID == "" {
		t.Fatal("WHYMISS_NETEM_TARGET_CONTAINER must name a Kurtosis service (e.g. el-1-geth-lighthouse)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dockerID, err := dockerContainerID(ctx, containerID)
	if err != nil {
		t.Fatalf("dockerContainerID(%s) error = %v", containerID, err)
	}
	veth, err := hostVethFor(ctx, dockerID)
	if err != nil {
		t.Fatalf("hostVethFor(%s) error = %v", dockerID, err)
	}
	t.Logf("resolved host veth: %s", veth)

	ip, err := containerIPForTest(ctx, dockerID)
	if err != nil {
		t.Fatalf("resolve container IP: %v", err)
	}

	baseline := pingRTT(t, ip)
	t.Logf("baseline RTT: %s", baseline)

	f := &NetemFault{Params: NetemParams{Delay: "300ms"}}
	revert, err := f.Apply(ctx, "whymiss-devnet", containerID)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	defer func() {
		if err := revert(ctx); err != nil {
			t.Errorf("revert() error = %v", err)
		}
	}()

	delayed := pingRTT(t, ip)
	t.Logf("delayed RTT: %s", delayed)

	if delayed < 250*time.Millisecond {
		t.Errorf("delayed RTT = %s, want at least ~300ms — the fault does not appear to have taken effect", delayed)
	}
	if delayed-baseline < 200*time.Millisecond {
		t.Errorf("RTT only grew by %s, want close to the requested 300ms delay", delayed-baseline)
	}
}

func containerIPForTest(ctx context.Context, containerID string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "inspect", containerID,
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func pingRTT(t *testing.T, ip string) time.Duration {
	t.Helper()
	out, err := exec.Command("ping", "-c", "3", "-W", "2", ip).Output()
	if err != nil {
		t.Fatalf("ping %s: %v", ip, err)
	}
	// Parse "rtt min/avg/max/mdev = 0.046/0.051/0.060/0.006 ms" for the avg value.
	text := string(out)
	i := strings.Index(text, "rtt min/avg/max")
	if i < 0 {
		t.Fatalf("could not find rtt summary in ping output:\n%s", text)
	}
	eq := strings.Index(text[i:], "=")
	slashParts := strings.Split(strings.TrimSpace(strings.SplitN(text[i+eq+1:], "ms", 2)[0]), "/")
	if len(slashParts) < 2 {
		t.Fatalf("could not parse rtt values from %q", text[i:])
	}
	avgMS, err := time.ParseDuration(slashParts[1] + "ms")
	if err != nil {
		t.Fatalf("parse avg rtt %q: %v", slashParts[1], err)
	}
	return avgMS
}
