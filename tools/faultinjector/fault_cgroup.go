package main

import (
	"context"
	"fmt"
	"os/exec"
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
// # Why this reaches into the VM
//
// A container's own view of /sys/fs/cgroup for itself is mounted read-only
// (verified: writing to it from inside the target container fails with "Read-only
// file system") — Docker does not delegate cgroup writes to containers by
// default, correctly, since a container writing its own resource limits upward
// would defeat the isolation those limits provide. The limit has to be set from
// outside the container, by whatever process cgroups says owns it.
//
// On Docker Desktop that "outside" is the LinuxKit VM Docker itself runs in, not
// the macOS host — verified by nsenter-ing into the VM's PID 1 and finding the
// container's real cgroup at /sys/fs/cgroup/docker/<container-id>/. A short-lived
// privileged helper container does the nsenter, since that is the standard,
// dependency-free way to reach a Docker-Desktop-for-Mac VM's namespaces (ADR-0004:
// no Kurtosis or Docker SDK needed — three CLI calls do the whole job). On a plain
// Linux host (the release target, I-13) this cgroup path is already visible
// directly; the helper container still works there, just doing less.
type CgroupIOFault struct {
	Params CgroupIOParams
}

// Apply resolves target's device and cgroup path, then writes the throttle.
func (f *CgroupIOFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	id, err := dockerContainerID(ctx, target)
	if err != nil {
		return nil, err
	}

	// cgroup v2's io controller throttles whole-disk devices, not partitions —
	// verified: writing io.max for /var/lib/docker's own device (a partition,
	// major:minor 254:1 in this project's test environment) fails with
	// "No such device", while its parent whole disk (254:0) accepts the write.
	// `lsblk -no pkname` reports the parent when the target is a partition, and
	// nothing when it is already a whole disk. `findmnt`'s SOURCE additionally
	// appends a "[/subpath]" annotation for a bind mount into a subvolume (seen
	// here as "/dev/vda1[/docker]"), which `${src%%[*}` strips before `basename`.
	//
	// One line, semicolon-separated rather than newline-separated: this string
	// crosses two nested `sh -c "..."` layers (the alpine helper's, then
	// nsenter's), and an embedded newline survives %q-quoting as the two literal
	// characters backslash-n, not an actual line break — the inner shell would
	// see one broken line instead of five statements.
	const resolveWholeDiskDevice = `src=$(findmnt -no SOURCE --target /var/lib/docker); ` +
		`src=${src%%[*}; ` +
		`part=$(basename "$src"); ` +
		`parent=$(lsblk -no pkname "/dev/$part"); ` +
		`[ -z "$parent" ] && parent=$part; ` +
		`cat "/sys/class/block/$parent/dev"`
	dev, err := hostNamespaceExec(ctx, resolveWholeDiskDevice)
	if err != nil {
		return nil, fmt.Errorf("resolve docker storage device: %w", err)
	}

	limitLine := cgroupIOMaxLine(dev, f.Params.ReadBytesPerSec, f.Params.WriteBytesPerSec)
	if err := writeCgroupIOMax(ctx, id, limitLine); err != nil {
		return nil, err
	}

	revert := func(ctx context.Context) error {
		return writeCgroupIOMax(ctx, id, cgroupIOMaxLine(dev, 0, 0))
	}
	return revert, nil
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

func writeCgroupIOMax(ctx context.Context, containerID, limitLine string) error {
	script := fmt.Sprintf(
		`echo '%s' > /sys/fs/cgroup/docker/%s/io.max`,
		limitLine, containerID,
	)
	if _, err := hostNamespaceExec(ctx, script); err != nil {
		return fmt.Errorf("write io.max for container %s: %w", containerID, err)
	}
	return nil
}

// hostNamespaceExec runs script inside the real Docker host's mount, PID, and
// network namespaces — the Docker-Desktop-for-Mac VM, or the host itself on
// native Linux — via a short-lived privileged helper container. See
// [CgroupIOFault] for why this is necessary.
func hostNamespaceExec(ctx context.Context, script string) (string, error) {
	// script must reach nsenter's inner shell byte-for-byte, including any `$`
	// it contains — those are meant to expand inside the host mount namespace
	// nsenter switches into, not in the alpine helper's own namespace one level
	// out. Go's %q verb double-quotes it, and a double-quoted argument is still
	// subject to $-expansion by the *outer* shell before nsenter ever runs,
	// which silently expands $(...) and $var against the wrong (empty)
	// filesystem view instead of passing them through literally. Single-quoting
	// is what actually defers all expansion to the inner shell; script has no
	// embedded single quotes today, so the standard POSIX escape for one
	// (close, escaped quote, reopen) is here defensively, not because it is
	// exercised yet.
	quoted := "'" + strings.ReplaceAll(script, "'", `'\''`) + "'"
	nsenterScript := fmt.Sprintf(
		`apk add --no-cache util-linux >/dev/null 2>&1; nsenter -t 1 -m -u -n -i sh -c %s`,
		quoted,
	)
	out, err := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--privileged", "--pid=host", "alpine", "sh", "-c", nsenterScript,
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("host-namespace exec %q: %w\n%s", script, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}
