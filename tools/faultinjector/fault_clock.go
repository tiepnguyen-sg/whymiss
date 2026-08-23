package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ClockSkewParams configures a fault that skews a client's perceived wall clock.
type ClockSkewParams struct {
	// Offset is the skew to apply, e.g. "+2s" or "-500ms" — parsed with
	// time.ParseDuration after stripping an optional leading sign.
	Offset string `yaml:"offset"`
}

// ClockSkewFault changes the offset file read by a target process that started
// with libfaketime preloaded. The devnet's Lighthouse validator image carries
// that setup; production images and the shipped whymiss binary do not.
type ClockSkewFault struct {
	Params ClockSkewParams
}

func (f *ClockSkewFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	id, err := dockerContainerID(ctx, target)
	if err != nil {
		return nil, err
	}

	offset, err := time.ParseDuration(f.Params.Offset)
	if err != nil || offset == 0 {
		return nil, fmt.Errorf("clock_skew: offset must be a non-zero duration, got %q", f.Params.Offset)
	}

	env, err := containerEnvironment(ctx, id)
	if err != nil {
		return nil, err
	}
	offsetFile := env["FAKETIME_TIMESTAMP_FILE"]
	if offsetFile == "" || !strings.Contains(env["LD_PRELOAD"], "libfaketime") {
		return nil, fmt.Errorf("clock_skew: target %s was not launched with libfaketime and FAKETIME_TIMESTAMP_FILE", target)
	}
	if err := containerPIDUsesLibfaketime(ctx, id); err != nil {
		return nil, err
	}

	original, err := readContainerFile(ctx, id, offsetFile)
	if err != nil {
		return nil, fmt.Errorf("clock_skew: read original offset: %w", err)
	}
	fakeOffset := formatFaketimeOffset(offset)
	if err := writeContainerFile(ctx, id, offsetFile, fakeOffset+"\n"); err != nil {
		return nil, fmt.Errorf("clock_skew: apply offset: %w", err)
	}

	revert := func(ctx context.Context) error {
		if err := writeContainerFile(ctx, id, offsetFile, string(original)); err != nil {
			return fmt.Errorf("clock_skew: restore original offset: %w", err)
		}
		return nil
	}
	return revert, nil
}

func formatFaketimeOffset(offset time.Duration) string {
	formatted := strconv.FormatFloat(float64(offset)/float64(time.Second), 'f', 9, 64)
	if offset > 0 {
		return "+" + formatted
	}
	return formatted
}

func containerEnvironment(ctx context.Context, containerID string) (map[string]string, error) {
	out, err := exec.CommandContext(ctx, "docker", "inspect", containerID,
		"--format", "{{range .Config.Env}}{{println .}}{{end}}",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("clock_skew: inspect target environment: %w", err)
	}
	env := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			env[key] = value
		}
	}
	return env, nil
}

func containerPIDUsesLibfaketime(ctx context.Context, containerID string) error {
	out, err := exec.CommandContext(ctx, "docker", "exec", containerID,
		"sh", "-c", `grep -q libfaketime /proc/1/maps`,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("clock_skew: target pid 1 has not loaded libfaketime: %w\n%s", err, out)
	}
	return nil
}

func readContainerFile(ctx context.Context, containerID, path string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "docker", "exec", containerID, "cat", path).Output()
	if err != nil {
		return nil, fmt.Errorf("docker exec %s cat %s: %w", containerID, path, err)
	}
	return out, nil
}

func writeContainerFile(ctx context.Context, containerID, path, content string) error {
	cmd := exec.CommandContext(ctx, "docker", "exec", containerID, "sh", "-c",
		`set -eu; tmp="$1.whymiss.$$"; trap 'rm -f "$tmp"' EXIT; printf '%s' "$2" > "$tmp"; chmod --reference="$1" "$tmp"; mv "$tmp" "$1"`,
		"clock-skew-write", path, content,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker exec %s update %s: %w\n%s", containerID, path, err, out)
	}
	return nil
}
