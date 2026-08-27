package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CgroupIOParams configures a disk-throttling fault via the cgroup v2 io
// controller.
type CgroupIOParams struct {
	// WriteBytesPerSec caps write throughput. Zero means unlimited (the field is
	// omitted from the io.max write).
	WriteBytesPerSec uint64 `yaml:"write_bytes_per_sec"`
	// ReadBytesPerSec caps read throughput. Zero means unlimited.
	ReadBytesPerSec uint64 `yaml:"read_bytes_per_sec"`
	// PressureBytes starts a devnet-only synchronous writer of this size in the
	// target cgroup, mirroring CgroupMemParams.PressureBytes and existing for
	// the same reason: a passive io.max cap alone produces no PSI signal.
	//
	// Measured on this project's devnet, with transaction load flowing. geth
	// writes about 40.8 KB/s in bursts of ~500 KB per block, and it writes them
	// buffered — the kernel accepts the write and flushes later, so throttling
	// writeback lets dirty pages accumulate without ever stalling the writer.
	// Caps of 1 MB/s, 128 KB/s, and 16 KB/s all left io.pressure at 0.00%. The
	// mechanism was never broken: capping the same cgroup at 16 KB/s and forcing
	// a synchronous write into it took io.pressure to some avg10=97.25%. What
	// was missing is something that actually blocks on the throttled device,
	// which is what this helper supplies.
	PressureBytes uint64 `yaml:"pressure_bytes,omitempty"`
}

// CgroupIOFault throttles a container's disk I/O by writing its cgroup v2
// io.max file — the same mechanism `systemd-run --property=IOWriteBandwidthMax`
// or a cloud provider's disk-IOPS cap uses, applied directly.
//
// # Why this needs the process itself to run privileged
//
// A container's own view of /sys/fs/cgroup for itself is mounted read-only
// (verified: writing to it from inside the target container fails with
// "Read-only file system") — Docker does not delegate cgroup writes to
// containers by default, correctly, since a container writing its own resource
// limits upward would defeat the isolation those limits provide. The limit has
// to be set from outside the container, by whatever process cgroups says owns
// it — for a container, that is the host.
//
// This package's process must therefore itself run on the real Linux host with
// root (matching NetemFault, which has the same requirement for the same
// reason: reaching a namespace the target container does not control). An
// earlier version reached the host indirectly through a `docker run
// --privileged --pid=host` helper container instead, needed on Docker Desktop
// for Mac where the Go process runs on macOS, not Linux, and has no other way
// to reach the Linux VM Docker actually runs in. That indirection was dropped
// after it proved unreliable in exactly this tool's real use (verified:
// `docker run --privileged --pid=host` hung intermittently even for a bare
// command with nothing installed, on a freshly rebooted host running nothing
// else — a genuine environment-level flakiness in nested privileged container
// creation, not a network hiccup or session-length effect, both of which were
// ruled out first). On a native Linux host, faultinjector already sees the
// real /sys/fs/cgroup directly — the indirection was never buying anything
// there, and removing it removes the only unreliable part of this fault.
type CgroupIOFault struct {
	Params CgroupIOParams
}

// Apply resolves target's device and cgroup path, then writes the throttle.
func (f *CgroupIOFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	id, err := dockerContainerID(ctx, enclave, target)
	if err != nil {
		return nil, err
	}

	dev, err := wholeDiskDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve docker storage device: %w", err)
	}

	cgroupDir, err := containerCgroupDir(id)
	if err != nil {
		return nil, err
	}
	original, err := os.ReadFile(filepath.Join(cgroupDir, "io.max"))
	if err != nil {
		return nil, fmt.Errorf("snapshot io.max: %w", err)
	}
	originalLine, ok := cgroupIOMaxDeviceLine(string(original), dev)
	if !ok {
		originalLine = cgroupIOMaxLine(dev, 0, 0)
	}

	limitLine := cgroupIOMaxLine(dev, f.Params.ReadBytesPerSec, f.Params.WriteBytesPerSec)
	if err := writeCgroupFile(cgroupDir, "io.max", limitLine); err != nil {
		return nil, err
	}

	// Started after the cap so the helper's very first write is already
	// throttled; a burst that lands unthrottled would drain before the duty
	// slot and leave nothing stalled to measure.
	var pressureHelper string
	if f.Params.PressureBytes > 0 {
		name, err := startCgroupIOPressure(ctx, id, f.Params.PressureBytes)
		if err != nil {
			if restoreErr := writeCgroupFile(cgroupDir, "io.max", originalLine); restoreErr != nil {
				return nil, errors.Join(err, restoreErr)
			}
			return nil, err
		}
		pressureHelper = name
	}

	revert := func(revertCtx context.Context) error {
		var stopErr error
		if pressureHelper != "" {
			stopErr = stopCgroupIOPressure(revertCtx, pressureHelper)
		}
		restoreErr := writeCgroupFile(cgroupDir, "io.max", originalLine)
		return errors.Join(stopErr, restoreErr)
	}
	return revert, nil
}

// startCgroupIOPressure runs a privileged helper that moves itself into the
// target container's cgroup and then writes synchronously in a loop, so its I/O
// is charged to — and throttled by — that cgroup's io.max. Mirrors
// startCgroupMemoryPressure; the difference is where it writes. The memory
// helper writes into a tmpfs, which is memory by definition; this one must land
// on the block device io.max caps, so it writes into its own container
// filesystem under /var/lib/docker rather than /tmp, and uses conv=fsync so the
// write blocks rather than returning to page cache.
func startCgroupIOPressure(ctx context.Context, containerID string, pressureBytes uint64) (string, error) {
	name := fmt.Sprintf("whymiss-io-pressure-%d-%d", os.Getpid(), time.Now().UnixNano())
	// The settle wait gives moveHelperIntoCgroup time to land before any I/O is
	// issued, so the helper's very first write is already charged to — and
	// throttled by — the target's cgroup.
	script := `
set -eu
pressure_bytes="$1"
sleep 2
load="/whymiss-io-pressure.load"
trap 'rm -f "$load"' EXIT INT TERM
count="$(( (pressure_bytes + 1048575) / 1048576 ))"
while :; do
	dd if=/dev/zero of="$load" bs=1048576 count="$count" conv=fsync >/dev/null 2>&1
	rm -f "$load"
done
`
	args := []string{
		"run", "--detach", "--rm", "--name", name, "--privileged", "--pid=host",
		dockerDesktopHelperImage, "sh", "-c", script, "io-pressure",
		fmt.Sprintf("%d", pressureBytes),
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("start cgroup io pressure helper: %w\n%s", err, out)
	}
	if err := moveHelperIntoCgroup(ctx, name, containerID); err != nil {
		return "", errors.Join(err, stopCgroupIOPressure(ctx, name))
	}
	return name, nil
}

// moveHelperIntoCgroup places a just-started helper container's process into
// target's cgroup, so the kernel charges the helper's resource use to the
// container under test.
//
// Done from here rather than from inside the helper, which is how it used to
// work and did not. The helper ran `nsenter -t 1 -m -- sh -c 'printf ... >
// cgroup.procs'` and that write failed with EIO every time, silently — the
// helper stayed in its own cgroup and its load was charged there. Proven by
// writing the same PID to the same file directly from the host, which succeeds
// (the cgroup is a `domain` leaf with empty subtree_control, so nothing about
// the target forbids the move). faultinjector already runs privileged on the
// host, so it can do the write itself and skip the namespace hop entirely.
//
// This was invisible for cgroup_mem because memory.high throttles the target's
// own processes directly, so PSI rose whether or not the helper joined. cgroup_io
// has no such effect — io.max alone never stalls a buffered writer — which is
// what finally exposed it.
func moveHelperIntoCgroup(ctx context.Context, helperName, targetContainerID string) error {
	out, err := exec.CommandContext(ctx, "docker", "inspect", helperName, "--format", "{{.State.Pid}}").Output()
	if err != nil {
		return fmt.Errorf("resolve helper %s pid: %w", helperName, err)
	}
	pid := strings.TrimSpace(string(out))
	if pid == "" || pid == "0" {
		return fmt.Errorf("resolve helper %s pid: got %q", helperName, pid)
	}
	if err := writeContainerCgroupFile(ctx, targetContainerID, "cgroup.procs", pid); err != nil {
		return fmt.Errorf("move helper %s into target cgroup: %w", helperName, err)
	}
	return nil
}

func stopCgroupIOPressure(ctx context.Context, name string) error {
	if out, err := exec.CommandContext(ctx, "docker", "stop", "--time", "1", name).CombinedOutput(); err != nil {
		return fmt.Errorf("stop cgroup io pressure helper %s: %w\n%s", name, err, out)
	}
	return nil
}

// wholeDiskDevice resolves the major:minor of the whole disk backing
// /var/lib/docker, run directly as a subprocess of this process — no nsenter,
// no helper container. This works because faultinjector itself runs on the
// real Linux host (see [CgroupIOFault]), so `findmnt`/`lsblk` here already see
// what the host sees.
//
// cgroup v2's io controller throttles whole-disk devices, not partitions —
// verified: writing io.max for /var/lib/docker's own device (a partition,
// major:minor 254:1 in this project's test environment) fails with "No such
// device", while its parent whole disk (254:0) accepts the write. `lsblk -no
// pkname` reports the parent when the target is a partition, and nothing when
// it is already a whole disk. `findmnt`'s SOURCE additionally appends a
// "[/subpath]" annotation for a bind mount into a subvolume (seen here as
// "/dev/vda1[/docker]"), stripped below before `basename`.
func wholeDiskDevice(ctx context.Context) (string, error) {
	srcOut, err := exec.CommandContext(ctx, "findmnt", "-no", "SOURCE", "--target", "/var/lib/docker").Output()
	if err != nil {
		return "", fmt.Errorf("findmnt /var/lib/docker: %w", err)
	}
	src := strings.TrimSpace(string(srcOut))
	if i := strings.IndexByte(src, '['); i >= 0 {
		src = src[:i]
	}
	part := filepath.Base(src)

	parentOut, err := exec.CommandContext(ctx, "lsblk", "-no", "pkname", "/dev/"+part).Output()
	if err != nil {
		return "", fmt.Errorf("lsblk /dev/%s: %w", part, err)
	}
	parent := strings.TrimSpace(string(parentOut))
	if parent == "" {
		parent = part
	}

	devOut, err := os.ReadFile(filepath.Join("/sys/class/block", parent, "dev"))
	if err != nil {
		return "", fmt.Errorf("read device number for %s: %w", parent, err)
	}
	return strings.TrimSpace(string(devOut)), nil
}

// containerCgroupDir locates containerID's cgroup v2 directory under
// /sys/fs/cgroup, read directly off the host filesystem (see [CgroupIOFault]).
//
// Docker's cgroup path depends on which cgroup driver the daemon uses:
// cgroupfs puts a container at /sys/fs/cgroup/docker/<id>/; systemd — the
// default on a stock Ubuntu host, and what this project's GCP verification VM
// actually runs — puts it at /sys/fs/cgroup/system.slice/docker-<id>.scope/
// instead. filepath.Glob covers both without needing to know which is active.
func containerCgroupDir(containerID string) (string, error) {
	matches, err := filepath.Glob("/sys/fs/cgroup/*/*" + containerID + "*")
	if err != nil {
		return "", fmt.Errorf("search for cgroup of container %s: %w", containerID, err)
	}
	if len(matches) == 0 {
		// cgroupfs nests one level shallower than systemd's slice layout.
		matches, err = filepath.Glob("/sys/fs/cgroup/*" + containerID + "*")
		if err != nil {
			return "", fmt.Errorf("search for cgroup of container %s: %w", containerID, err)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no cgroup found for container %s", containerID)
	}
	return matches[0], nil
}

// cgroupIOMaxLine formats one line of cgroup v2 io.max: "<major:minor>
// rbps=<n|max> wbps=<n|max>". Zero means "max" (no limit) rather than "0 bytes/s",
// since io.max treats 0 as invalid input, not as "unlimited".
func cgroupIOMaxLine(dev string, readBps, writeBps uint64) string {
	r, w := "max", "max"
	if readBps > 0 {
		r = fmt.Sprintf("%d", readBps)
	}
	if writeBps > 0 {
		w = fmt.Sprintf("%d", writeBps)
	}
	return fmt.Sprintf("%s rbps=%s wbps=%s", dev, r, w)
}

func cgroupIOMaxDeviceLine(content, dev string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == dev {
			return strings.Join(fields, " "), true
		}
	}
	return "", false
}

// writeCgroupFile writes content to <cgroupDir>/<file>, direct os.WriteFile —
// no shell, no helper process. A cgroup control file always accepts a single
// write() of its whole new value; there is nothing here for a shell to do that
// os.WriteFile does not already do more simply and more reliably.
func writeCgroupFile(cgroupDir, file, content string) error {
	path := filepath.Join(cgroupDir, file)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // G306: a cgroup control file's permissions are fixed by the kernel, not by this write
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
