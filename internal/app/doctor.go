package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
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

	// CLMetricsAPI, BaselineBeaconAPI, and BaselineMetricsAPI are the endpoints
	// that decide whether attribution is possible at all, as opposed to whether
	// collection works. They are checked because an operator who sees every other
	// check green has no way to tell that no timing cause will ever be reported.
	CLMetricsAPI       string
	BaselineBeaconAPI  string
	BaselineMetricsAPI string
}

// DoctorCheck is one independently actionable preflight result.
type DoctorCheck struct {
	Name   string
	Detail string
	Err    error

	// Warn marks a check that found a real limitation in a configuration that is
	// nonetheless legitimate: an endpoint left unset narrows what whymiss can
	// conclude without being a misconfiguration. A warning is reported and does
	// not fail the command, where an endpoint that was configured and does not
	// work is an Err — the operator asked for it and it is not there.
	Warn bool
}

// Doctor verifies every configured endpoint, plus the two prerequisites that are
// not endpoints: a writable store location and a fresh NTP reading.
//
// It reports on the attribution endpoints as well as the collection ones. Doctor
// used to check only what it took to collect — beacon API, store, clock — which
// meant an operator could watch every check pass and then get
// unknown.insufficient_data on every degraded duty, because without a reachable
// CL metrics endpoint no stage of a duty is ever timed (ADR-0024) and without an
// independent baseline the product's central question cannot be answered at all
// (ADR-0025). Those are exactly the conditions worth learning at setup rather
// than during the incident being diagnosed.
//
// It performs read-only calls against operator-configured endpoints only, and no
// unconfigured network egress (I-1, I-4).
func Doctor(ctx context.Context, cfg DoctorConfig) []DoctorCheck {
	checks := make([]DoctorCheck, 0, 5)

	if cfg.BeaconAPI == "" || cfg.DBPath == "" || cfg.MinRequestInterval <= 0 || cfg.ClockOffsetMax <= 0 {
		checks = append(checks, DoctorCheck{Name: "config", Err: fmt.Errorf("beacon API, database path, request interval, and clock threshold are required")})
		return checks
	}
	if err := validateHTTPEndpoint("beacon API", cfg.BeaconAPI); err != nil {
		checks = append(checks, DoctorCheck{Name: "config", Err: err})
		return checks
	}

	client := beaconapi.NewClient(cfg.BeaconAPI, cfg.MinRequestInterval)
	var version string
	genesis, err := client.FetchGenesis(ctx)
	if err == nil {
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

	checks = append(checks, checkCLMetrics(ctx, cfg, version))
	checks = append(checks, checkBaseline(ctx, cfg)...)

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

// checkCLMetrics reports whether local stage timing is available. An unset
// endpoint is a warning naming what becomes unreportable; a configured endpoint
// that cannot be scraped for this client's arrival gauge is an error.
func checkCLMetrics(ctx context.Context, cfg DoctorConfig, nodeVersion string) DoctorCheck {
	if cfg.CLMetricsAPI == "" {
		return DoctorCheck{
			Name: "metrics",
			Warn: true,
			Detail: "no --cl-metrics-api: no stage of a duty is timed, so local.cl_slow, " +
				"local.el_slow, local.vc_slow, local.vc_disconnected, network.late_block and " +
				"local.p2p_degraded can never be reported (ADR-0024). Collection still works.",
		}
	}
	if err := validateHTTPEndpoint("CL metrics API", cfg.CLMetricsAPI); err != nil {
		return DoctorCheck{Name: "metrics", Err: err}
	}
	if nodeVersion == "" {
		return DoctorCheck{Name: "metrics", Err: fmt.Errorf(
			"cannot check the CL metrics API because the beacon API did not report a node version to identify the client by")}
	}
	consensus := source.DetectConsensusClient(nodeVersion)
	if consensus == source.ConsensusUnknown {
		return DoctorCheck{Name: "metrics", Err: fmt.Errorf(
			"consensus client %q is not one this build can read metrics from; block arrival and Engine timings would be unavailable", nodeVersion)}
	}
	timing, err := source.NewMetricsSampler().SampleBlockTiming(ctx, consensus, cfg.CLMetricsAPI)
	if err != nil {
		return DoctorCheck{Name: "metrics", Err: fmt.Errorf("scrape block arrival from the CL metrics API: %w", err)}
	}
	return DoctorCheck{
		Name:   "metrics",
		Detail: fmt.Sprintf("scraped; client=%s head_slot=%d block_arrival=%s", consensus, timing.Slot, timing.Propagation),
	}
}

// checkBaseline reports whether the independent comparison the product's central
// question needs is available. It returns one check for the baseline beacon API
// and, when configured, a second for its metrics endpoint, because they fail for
// different reasons and an operator fixes them separately.
func checkBaseline(ctx context.Context, cfg DoctorConfig) []DoctorCheck {
	if cfg.BaselineBeaconAPI == "" {
		if cfg.BaselineMetricsAPI != "" {
			// Mirrors watchConfig.validate: a metrics endpoint alone names no node.
			return []DoctorCheck{{Name: "baseline", Err: fmt.Errorf(
				"baseline metrics API needs a baseline beacon API to name the node it belongs to")}}
		}
		return []DoctorCheck{{
			Name: "baseline",
			Warn: true,
			Detail: "no --baseline-beacon-api: nothing independent to compare local timing against, " +
				"so network.late_block and local.p2p_degraded decline and \"was it me or the network\" " +
				"resolves to unknown.insufficient_data (ADR-0025).",
		}}
	}
	if err := validateHTTPEndpoint("baseline beacon API", cfg.BaselineBeaconAPI); err != nil {
		return []DoctorCheck{{Name: "baseline", Err: err}}
	}
	if sameHTTPEndpoint(cfg.BaselineBeaconAPI, cfg.BeaconAPI) {
		return []DoctorCheck{{Name: "baseline", Err: fmt.Errorf(
			"baseline beacon API must be a different node than the watched beacon API; the same node would report local lateness as network-wide and exonerate a real local fault")}}
	}

	baseline := beaconapi.NewClient(cfg.BaselineBeaconAPI, cfg.MinRequestInterval)
	version, err := baseline.FetchNodeVersion(ctx)
	if err != nil {
		return []DoctorCheck{{Name: "baseline", Err: fmt.Errorf("reach the baseline beacon API: %w", err)}}
	}
	checks := []DoctorCheck{{
		Name:   "baseline",
		Detail: fmt.Sprintf("connected; client=%s (arrival read from its Beacon API)", version),
	}}
	if cfg.BaselineMetricsAPI == "" {
		return checks
	}

	if err := validateHTTPEndpoint("baseline metrics API", cfg.BaselineMetricsAPI); err != nil {
		return append(checks, DoctorCheck{Name: "baseline+", Err: err})
	}
	consensus := source.DetectConsensusClient(version)
	if consensus == source.ConsensusUnknown {
		return append(checks, DoctorCheck{Name: "baseline+", Err: fmt.Errorf(
			"baseline consensus client %q is not one this build can read metrics from; unset --baseline-metrics-api to read arrival from its Beacon API instead", version)})
	}
	timing, err := source.NewMetricsSampler().SampleBlockTiming(ctx, consensus, cfg.BaselineMetricsAPI)
	if err != nil {
		return append(checks, DoctorCheck{Name: "baseline+", Err: fmt.Errorf("scrape block arrival from the baseline metrics API: %w", err)})
	}
	return append(checks, DoctorCheck{
		Name:   "baseline+",
		Detail: fmt.Sprintf("scraped; head_slot=%d block_arrival=%s (millisecond precision)", timing.Slot, timing.Propagation),
	})
}
