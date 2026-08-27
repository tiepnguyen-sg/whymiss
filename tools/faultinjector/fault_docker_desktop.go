package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// The helper runs only in the devnet-only injector. Pinning the digest keeps
// privileged fallback behavior reproducible across corpus regeneration runs.
const dockerDesktopHelperImage = "alpine@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"

func dockerDesktopFaultFallback(kind string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	switch kind {
	case "netem", "cgroup_cpu", "cgroup_mem":
		return true
	default:
		return false
	}
}

func runDockerDesktopNetem(ctx context.Context, containerID, action, delay, loss, peerIPs string) error {
	pidOut, err := exec.CommandContext(ctx, "docker", "inspect", containerID, "--format", "{{.State.Pid}}").Output()
	if err != nil {
		return fmt.Errorf("resolve Docker Desktop pid for container %s: %w", containerID, err)
	}
	pid := strings.TrimSpace(string(pidOut))
	if pid == "" {
		return fmt.Errorf("resolve Docker Desktop pid for container %s: empty result", containerID)
	}

	script := `
set -eu
pid="$1"
action="$2"
delay="$3"
loss="$4"
peer_ips="$5"
peer="$(nsenter -t "$pid" -n -- ip -o link show eth0 | sed -n 's/.*eth0@if\([0-9][0-9]*\).*/\1/p')"
[ -n "$peer" ]
veth="$(nsenter -t 1 -n -- ip -o link show | awk -v target="$peer" '$1 == target ":" { name=$2; sub(/@.*/, "", name); print name; exit }')"
[ -n "$veth" ]
# Docker Desktop's Linux VM already ships tc. Enter its mount namespace as
# well as its network namespace so the pinned helper never downloads packages
# or depends on an external APK mirror while a fault is being applied.
tc_vm() { nsenter -t 1 -m -n -- tc "$@"; }
if [ "$action" = "del" ]; then
	exec nsenter -t 1 -m -n -- tc qdisc del dev "$veth" root
fi

cleanup=false
trap 'if [ "$cleanup" = true ]; then tc_vm qdisc del dev "$veth" root >/dev/null 2>&1 || true; fi' EXIT
if [ -z "$peer_ips" ]; then
	set -- qdisc add dev "$veth" root netem
	[ -z "$delay" ] || set -- "$@" delay "$delay"
	[ -z "$loss" ] || set -- "$@" loss "$loss"
	tc_vm "$@"
	exit 0
fi

tc_vm qdisc add dev "$veth" root handle 1: prio bands 4 priomap 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0
cleanup=true
set -- qdisc add dev "$veth" parent 1:4 handle 40: netem
[ -z "$delay" ] || set -- "$@" delay "$delay"
[ -z "$loss" ] || set -- "$@" loss "$loss"
tc_vm "$@"
for peer_ip in $peer_ips; do
	tc_vm filter add dev "$veth" protocol ip parent 1: prio 1 u32 match ip src "$peer_ip/32" flowid 1:4
done
cleanup=false
`
	args := dockerDesktopHelperArgs("sh", "-c", script, "netem", pid, action, delay, loss, peerIPs)
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("run Docker Desktop tc qdisc %s for container %s: %w\n%s", action, containerID, err, out)
	}
	return nil
}

// writeContainerCgroupFile writes through the native host on Linux and through
// a short-lived privileged helper inside Docker Desktop's Linux VM on macOS.
// Access to the Docker socket is already host-equivalent authority; the shipped
// whymiss binary never contains or calls this code.
func writeContainerCgroupFile(ctx context.Context, containerID, file, content string) error {
	if runtime.GOOS != "darwin" {
		cgroupDir, err := containerCgroupDir(containerID)
		if err != nil {
			return err
		}
		return writeCgroupFile(cgroupDir, file, content)
	}

	script := `for candidate in "/sys/fs/cgroup/docker/$1" "/sys/fs/cgroup/system.slice/docker-$1.scope"; do if [ -d "$candidate" ]; then printf "%s" "$3" > "$candidate/$2"; exit 0; fi; done; exit 1`
	args := dockerDesktopHelperArgs(
		"nsenter", "-t", "1", "-m", "--", "sh", "-c", script,
		"cgroup-write", containerID, file, content,
	)
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("write Docker Desktop cgroup file %s for container %s: %w\n%s", file, containerID, err, out)
	}
	return nil
}

func readContainerCgroupFile(ctx context.Context, containerID, file string) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		cgroupDir, err := containerCgroupDir(containerID)
		if err != nil {
			return nil, err
		}
		out, err := exec.CommandContext(ctx, "cat", cgroupDir+"/"+file).Output()
		if err != nil {
			return nil, fmt.Errorf("read cgroup file %s for container %s: %w", file, containerID, err)
		}
		return out, nil
	}

	script := `for candidate in "/sys/fs/cgroup/docker/$1" "/sys/fs/cgroup/system.slice/docker-$1.scope"; do if [ -d "$candidate" ]; then cat "$candidate/$2"; exit 0; fi; done; exit 1`
	args := dockerDesktopHelperArgs(
		"nsenter", "-t", "1", "-m", "--", "sh", "-c", script,
		"cgroup-read", containerID, file,
	)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("read Docker Desktop cgroup file %s for container %s: %w\n%s", file, containerID, err, out)
	}
	return out, nil
}

func dockerDesktopHelperArgs(command ...string) []string {
	args := []string{"run", "--rm", "--privileged", "--pid=host", dockerDesktopHelperImage}
	return append(args, command...)
}
