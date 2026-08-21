package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CgroupIOParams configures a disk-throttling fault via the cgroup v2 io
// controller.
type CgroupIOParams struct {
	// WriteBytesPerSec caps write throughput. Zero means unlimited (the field is
	// omitted from the io.max write).
	WriteBytesPerSec uint64 `yaml:"write_bytes_per_sec"`
	// ReadBytesPerSec caps read throughput. Zero means unlimited.
	ReadBytesPerSec uint64 `yaml:"read_bytes_per_sec"`
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
	id, err := dockerContainerID(ctx, target)
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

	limitLine := cgroupIOMaxLine(dev, f.Params.ReadBytesPerSec, f.Params.WriteBytesPerSec)
	if err := writeCgroupFile(cgroupDir, "io.max", limitLine); err != nil {
		return nil, err
	}

	revert := func(context.Context) error {
		return writeCgroupFile(cgroupDir, "io.max", cgroupIOMaxLine(dev, 0, 0))
	}
	return revert, nil
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
