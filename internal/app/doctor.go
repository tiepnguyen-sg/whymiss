package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

// DoctorConfig is the operator-supplied connectivity configuration Doctor checks.
type DoctorConfig struct {
	BeaconAPI          string
	DBPath             string
	NTPServers         []string
	MinRequestInterval time.Duration
	ClockOffsetMax     time.Duration
}

// DoctorCheck is one independently actionable preflight result.
type DoctorCheck struct {
	Name   string
	Detail string
	Err    error
}

// Doctor verifies the three prerequisites required for trustworthy operation:
// beacon API connectivity, a writable store location, and a fresh NTP reading.
// It performs read-only beacon calls and no unconfigured network egress.
func Doctor(ctx context.Context, cfg DoctorConfig) []DoctorCheck {
	checks := make([]DoctorCheck, 0, 3)

	if cfg.BeaconAPI == "" || cfg.DBPath == "" || cfg.MinRequestInterval <= 0 || cfg.ClockOffsetMax <= 0 {
		checks = append(checks, DoctorCheck{Name: "config", Err: fmt.Errorf("beacon API, database path, request interval, and clock threshold are required")})
		return checks
	}
	if err := validateHTTPEndpoint("beacon API", cfg.BeaconAPI); err != nil {
		checks = append(checks, DoctorCheck{Name: "config", Err: err})
		return checks
	}

	client := beaconapi.NewClient(cfg.BeaconAPI, cfg.MinRequestInterval)
	genesis, err := client.FetchGenesis(ctx)
	if err == nil {
		var version string
		version, err = client.FetchNodeVersion(ctx)
		if err == nil {
			checks = append(checks, DoctorCheck{
				Name:   "beacon",
				Detail: fmt.Sprintf("connected; client=%s genesis=%s slot=%s", version, genesis.GenesisTime.Format(time.RFC3339), genesis.SecondsPerSlot),
			})
		}
	}
	if err != nil {
		checks = append(checks, DoctorCheck{Name: "beacon", Err: err})
	}

	if err := checkDBPath(ctx, cfg.DBPath); err != nil {
		checks = append(checks, DoctorCheck{Name: "database", Err: err})
	} else {
		checks = append(checks, DoctorCheck{Name: "database", Detail: "path is writable"})
	}

	if len(cfg.NTPServers) == 0 {
		checks = append(checks, DoctorCheck{Name: "clock", Err: fmt.Errorf("no NTP server configured; timing attribution would be disabled")})
		return checks
	}
	sampler, err := clock.New(clock.Config{Servers: cfg.NTPServers, Timeout: 5 * time.Second, MaxAttempts: 3})
	if err != nil {
		checks = append(checks, DoctorCheck{Name: "clock", Err: err})
		return checks
	}
	reading, err := sampler.Sample(ctx)
	if err != nil {
		checks = append(checks, DoctorCheck{Name: "clock", Err: err})
		return checks
	}
	offset := absoluteDuration(reading.Offset)
	limit := cfg.ClockOffsetMax
	if offset > limit {
		checks = append(checks, DoctorCheck{Name: "clock", Err: fmt.Errorf("offset %s from %s exceeds trust limit %s", reading.Offset, reading.Server, limit)})
		return checks
	}
	checks = append(checks, DoctorCheck{
		Name:   "clock",
		Detail: fmt.Sprintf("server=%s offset=%s round_trip=%s", reading.Server, reading.Offset, reading.RoundTrip),
	})
	return checks
}

func checkDBPath(ctx context.Context, path string) error {
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		return fmt.Errorf("database path is a directory")
	case err == nil:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("database path is not a regular file")
		}
		f, openErr := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // operator-configured database path is the purpose of this check
		if openErr != nil {
			return fmt.Errorf("open existing database for writing: %w", openErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			return fmt.Errorf("close existing database: %w", closeErr)
		}
		st, openErr := store.Open(ctx, path)
		if openErr != nil {
			return fmt.Errorf("open existing whymiss database: %w", openErr)
		}
		if closeErr := st.Close(); closeErr != nil {
			return fmt.Errorf("close existing whymiss database: %w", closeErr)
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("inspect database path: %w", err)
	}

	dir := filepath.Dir(path)
	probe, err := os.CreateTemp(dir, ".whymiss-doctor-*")
	if err != nil {
		return fmt.Errorf("create file in database directory: %w", err)
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		closeErr := fmt.Errorf("close database-directory probe: %w", err)
		if removeErr := os.Remove(probePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return errors.Join(closeErr, fmt.Errorf("remove database-directory probe: %w", removeErr))
		}
		return closeErr
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("remove database-directory probe: %w", err)
	}
	return nil
}
