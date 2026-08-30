package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/exporter"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
	"github.com/tiepnguyen-sg/whymiss/internal/source/hostmetrics"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

const maxTrackedValidators = 64

const maxMetricsConnections = 16

// WatchConfig is whymiss watch's runtime configuration.
type WatchConfig struct {
	// BeaconAPI is the beacon node's base URL, e.g. "http://127.0.0.1:5052".
	BeaconAPI string

	// DBPath is the SQLite file watch reads and writes.
	DBPath string

	// MinRequestInterval is the floor between successive beacon API
	// requests (I-5); see beaconapi.NewClient.
	MinRequestInterval time.Duration

	// HostSampleInterval is how often disk/memory/CPU pressure is sampled.
	// Zero disables host sampling — meaningful when whymiss does not run on
	// the staking box itself, or during development on a platform without
	// /proc (I-3: this degrades to "no host samples", not a crash).
	HostSampleInterval time.Duration

	// CLMetricsAPI is the consensus client's own Prometheus endpoint, e.g.
	// "http://127.0.0.1:5054/metrics". Empty disables peer-count sampling —
	// meaningful when the node's Prometheus port isn't exposed, or during
	// development against a node that doesn't have one configured (I-3:
	// degrades cleanly, not a crash).
	CLMetricsAPI string

	// PeerSampleInterval is how often the watched node's connected peer count is
	// read. The count comes from the Beacon API's /eth/v1/node/peer_count
	// (ADR-0023), not from CLMetricsAPI, so this sampling runs whether or not a
	// metrics endpoint is configured.
	PeerSampleInterval time.Duration

	// BaselineBeaconAPI and BaselineMetricsAPI point at a second, independent
	// beacon node — one that does not share this node's peers or host. Its
	// block-arrival timing is what separates "late for the whole network"
	// from "late here" (R-110, R-200); without it both rules refuse to
	// attribute rather than guess. Both empty disables baseline collection,
	// which is the default: pointing at another node is explicit operator
	// configuration, never implicit egress (I-4).
	BaselineBeaconAPI  string
	BaselineMetricsAPI string

	// NTPServers are the servers the clock sampler queries. Empty disables
	// clock sampling, which leaves every observation unmeasured and makes
	// timing rules report insufficient data rather than trust the local
	// clock (I-9). There is no built-in default: no unconfigured egress (I-4).
	NTPServers []string

	// ClockSampleInterval is how often NTPServers are queried.
	ClockSampleInterval time.Duration

	// RetentionMaxAge and RetentionMaxBytes are store.Prune's limits,
	// applied on RetentionInterval (I-12).
	RetentionMaxAge   time.Duration
	RetentionMaxBytes int64
	RetentionInterval time.Duration

	// ValidatorIndices, when non-empty, are the validators whose attester
	// duties this loop tracks: each epoch, their duty slots are fetched
	// and, as each one completes, polled for block_seen/
	// attestation_published/attestation_included and run through
	// rca.Analyze (via Explain) — the only way this daemon produces a
	// domain.Verdict at all. Empty disables duty tracking and the
	// exporter entirely (I-3: this loop still runs, just as a pure
	// observation collector, exactly as it did before task 4.1).
	ValidatorIndices []domain.ValidatorIndex

	// MetricsAddr, when set, serves the exporter's Prometheus metrics at
	// "<MetricsAddr>/metrics". Meaningless (ignored) when
	// ValidatorIndices is empty, since there is nothing to export.
	MetricsAddr string

	// Schedule is passed to Explain for every completed duty. Defaults to
	// domain.MainnetPreEPBS() when zero. Operators can override the schedule
	// through the strict config file or environment variables.
	Schedule domain.SlotSchedule

	// RCAConfig contains the documented cause thresholds.
	RCAConfig rca.Config

	Logger *slog.Logger
}

// Validate reports configuration that would make Watch unsafe or panic. Zero is
// accepted only for sampling/retention intervals whose documented meaning is
// "disabled"; an enabled source always requires a positive interval (I-15).
func (c WatchConfig) Validate() error {
	const (
		minRequestInterval = 100 * time.Millisecond
		maxRequestInterval = 2 * time.Second
		minHostInterval    = 5 * time.Second
		maxHostInterval    = time.Minute
		minPeerInterval    = 5 * time.Second
		maxPeerInterval    = time.Minute
		minClockInterval   = 10 * time.Second
		maxClockInterval   = time.Minute
		minRetentionAge    = 24 * time.Hour
		maxRetentionAge    = 90 * 24 * time.Hour
		minRetentionBytes  = 100 << 20
		maxRetentionBytes  = 10 << 30
		minRetentionPeriod = 5 * time.Minute
		maxRetentionPeriod = 24 * time.Hour
	)
	if c.BeaconAPI == "" {
		return fmt.Errorf("beacon API is required")
	}
	if err := validateHTTPEndpoint("beacon API", c.BeaconAPI); err != nil {
		return err
	}
	if c.DBPath == "" {
		return fmt.Errorf("database path is required")
	}
	if c.MinRequestInterval < minRequestInterval || c.MinRequestInterval > maxRequestInterval {
		return fmt.Errorf("minimum request interval must be between %s and %s, got %s", minRequestInterval, maxRequestInterval, c.MinRequestInterval)
	}
	if c.HostSampleInterval != 0 && (c.HostSampleInterval < minHostInterval || c.HostSampleInterval > maxHostInterval) {
		return fmt.Errorf("host sample interval must be zero or between %s and %s, got %s", minHostInterval, maxHostInterval, c.HostSampleInterval)
	}
	// Validated unconditionally: peer sampling no longer depends on CLMetricsAPI,
	// so an out-of-range interval now takes effect on every deployment rather
	// than only on metrics-enabled ones.
	if c.PeerSampleInterval < minPeerInterval || c.PeerSampleInterval > maxPeerInterval {
		return fmt.Errorf("peer sample interval must be between %s and %s, got %s", minPeerInterval, maxPeerInterval, c.PeerSampleInterval)
	}
	if c.CLMetricsAPI != "" {
		if err := validateHTTPEndpoint("CL metrics API", c.CLMetricsAPI); err != nil {
			return err
		}
	}
	// --baseline-metrics-api is optional: without it the baseline is measured
	// from the independent node's Beacon API instead of its Prometheus surface
	// (ADR-0025), which is what lets an operator use a node they can reach
	// rather than one they run. The reverse is still nonsense — a metrics
	// endpoint with no Beacon API names no node to compare against.
	if c.BaselineBeaconAPI == "" && c.BaselineMetricsAPI != "" {
		return fmt.Errorf("baseline metrics API needs a baseline beacon API to name the node it belongs to")
	}
	if c.BaselineBeaconAPI != "" {
		if err := validateHTTPEndpoint("baseline beacon API", c.BaselineBeaconAPI); err != nil {
			return err
		}
		if c.BaselineMetricsAPI != "" {
			if err := validateHTTPEndpoint("baseline metrics API", c.BaselineMetricsAPI); err != nil {
				return err
			}
		}
		// A baseline that is just this node again proves nothing: it would
		// report local lateness as network-wide lateness and exonerate a real
		// local fault.
		if sameHTTPEndpoint(c.BaselineBeaconAPI, c.BeaconAPI) {
			return fmt.Errorf("baseline beacon API must be a different node than the watched beacon API")
		}
	}
	if len(c.NTPServers) > 0 && (c.ClockSampleInterval < minClockInterval || c.ClockSampleInterval > maxClockInterval) {
		return fmt.Errorf("clock sample interval must be between %s and %s when NTP is enabled, got %s", minClockInterval, maxClockInterval, c.ClockSampleInterval)
	}
	if c.RetentionInterval > 0 {
		if c.RetentionInterval < minRetentionPeriod || c.RetentionInterval > maxRetentionPeriod {
			return fmt.Errorf("retention interval must be zero or between %s and %s, got %s", minRetentionPeriod, maxRetentionPeriod, c.RetentionInterval)
		}
		if c.RetentionMaxAge < minRetentionAge || c.RetentionMaxAge > maxRetentionAge {
			return fmt.Errorf("retention max age must be between %s and %s, got %s", minRetentionAge, maxRetentionAge, c.RetentionMaxAge)
		}
		if c.RetentionMaxBytes < minRetentionBytes || c.RetentionMaxBytes > maxRetentionBytes {
			return fmt.Errorf("retention max bytes must be between %d and %d, got %d", minRetentionBytes, maxRetentionBytes, c.RetentionMaxBytes)
		}
	} else if c.RetentionInterval < 0 {
		return fmt.Errorf("retention interval cannot be negative, got %s", c.RetentionInterval)
	}
	schedule := c.Schedule
	if schedule == (domain.SlotSchedule{}) {
		schedule = domain.MainnetPreEPBS()
	}
	if err := schedule.Validate(); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	rcaConfig := c.RCAConfig
	if rcaConfig == (rca.Config{}) {
		rcaConfig = rca.DefaultConfig()
	}
	if err := rcaConfig.Validate(); err != nil {
		return fmt.Errorf("RCA config: %w", err)
	}
	if len(c.ValidatorIndices) > maxTrackedValidators {
		return fmt.Errorf("validator index count must not exceed %d, got %d", maxTrackedValidators, len(c.ValidatorIndices))
	}
	seenValidators := make(map[domain.ValidatorIndex]struct{}, len(c.ValidatorIndices))
	for _, index := range c.ValidatorIndices {
		if _, exists := seenValidators[index]; exists {
			return fmt.Errorf("validator index %d is configured more than once", index)
		}
		seenValidators[index] = struct{}{}
	}
	return nil
}

func validateHTTPEndpoint(name, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%s must be an absolute http or https URL", name)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not contain embedded credentials", name)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s base URL must not contain a query or fragment", name)
	}
	return nil
}

func sameHTTPEndpoint(left, right string) bool {
	canonical := func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return raw
		}
		port := parsed.Port()
		if port == "" {
			switch strings.ToLower(parsed.Scheme) {
			case "http":
				port = "80"
			case "https":
				port = "443"
			}
		}
		path := strings.TrimRight(parsed.EscapedPath(), "/")
		return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Hostname()) + ":" + port + path
	}
	return canonical(left) == canonical(right)
}

func validateBaselineGenesis(watched, baseline beaconapi.GenesisInfo) error {
	if !baseline.GenesisTime.Equal(watched.GenesisTime) || baseline.SecondsPerSlot != watched.SecondsPerSlot {
		return fmt.Errorf("baseline beacon node is on a different network: genesis_time=%s seconds_per_slot=%s; watched genesis_time=%s seconds_per_slot=%s",
			baseline.GenesisTime, baseline.SecondsPerSlot, watched.GenesisTime, watched.SecondsPerSlot)
	}
	return nil
}

// Watch runs the collector daemon until ctx is done: it streams the beacon
// node's head/chain_reorg events, optionally samples host resource
// pressure, and persists everything to cfg.DBPath, pruning on a timer.
// When cfg.ValidatorIndices is non-empty, it also tracks each validator's
// attester duties, polls their outcome, runs every completed duty through
// rca.Analyze (via Explain), and records the result into a Prometheus
// exporter — see duty_tracking.go and internal/exporter.
func Watch(ctx context.Context, cfg WatchConfig) (retErr error) {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate watch config: %w", err)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx = runCtx
	rcaConfig := cfg.RCAConfig
	if rcaConfig == (rca.Config{}) {
		rcaConfig = rca.DefaultConfig()
	}

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close store: %w", err))
		}
	}()

	client := beaconapi.NewClient(cfg.BeaconAPI, cfg.MinRequestInterval)
	metricsSampler := source.NewMetricsSampler()
	genesis, err := client.FetchGenesis(ctx)
	if err != nil {
		return fmt.Errorf("fetch genesis: %w", err)
	}
	logger.Info("connected to beacon node", "beacon_api", cfg.BeaconAPI, "genesis_time", genesis.GenesisTime)
	schedule := cfg.Schedule
	if schedule == (domain.SlotSchedule{}) {
		schedule = domain.MainnetPreEPBS()
	}
	schedule = adoptNodeSchedule(ctx, client, genesis, schedule, logger)

	streamState := newStreamHealth(func() time.Time { return time.Now().UTC() })
	events := client.Stream(ctx, func(streamErr error) {
		streamState.failed(logger, streamErr)
	})
	var background sync.WaitGroup
	fatalErrors := make(chan error, 1)
	start := func(fn func()) {
		background.Add(1)
		go func() {
			defer background.Done()
			fn()
		}()
	}
	defer func() {
		cancel()
		for range events {
		}
		background.Wait()
	}()

	var clk *clock.Tracker
	var clockMaxAge time.Duration
	if len(cfg.NTPServers) > 0 {
		sampler, err := clock.New(clock.Config{
			Servers: cfg.NTPServers, Timeout: 5 * time.Second, MaxAttempts: 3,
		})
		if err != nil {
			return fmt.Errorf("configure clock sampler: %w", err)
		}
		clk, err = clock.NewTracker(sampler)
		if err != nil {
			return fmt.Errorf("configure clock tracker: %w", err)
		}
		clockMaxAge = rcaConfig.ClockSampleMaxAge
		start(func() { runClockSampler(ctx, clk, cfg.ClockSampleInterval, logger) })
	} else {
		logger.Warn("no NTP server configured; timing attribution will report insufficient data (I-9)")
	}

	if cfg.RetentionInterval > 0 {
		start(func() { runRetention(ctx, st, cfg, logger) })
	}
	if cfg.HostSampleInterval > 0 {
		start(func() { runHostSampling(ctx, st, cfg.HostSampleInterval, clk, clockMaxAge, logger) })
	}
	// Peer sampling reads /eth/v1/node/peer_count on the watched node's own Beacon
	// API (ADR-0023), so gating it behind CLMetricsAPI denied R-200's peer
	// corroboration to every operator without a metrics endpoint for no reason
	// the measurement required. It is started on its own.
	start(func() {
		runPeerSampling(ctx, st, client, cfg.PeerSampleInterval, clk, clockMaxAge, logger)
	})

	var heads headFanout
	if cfg.CLMetricsAPI != "" {
		versionString, err := client.FetchNodeVersion(ctx)
		if err != nil {
			return fmt.Errorf("fetch node version: %w", err)
		}
		consensusClient := source.DetectConsensusClient(versionString)
		if consensusClient == source.ConsensusUnknown {
			return fmt.Errorf("CL metrics collection is not supported for consensus client version %q", versionString)
		}
		logger.Info("detected consensus client", "version", versionString, "client", consensusClient)
		// One pending head bounds memory if metrics scraping stalls. Dropped
		// timing work degrades to unknown rather than delaying collection.
		heads.timing = make(chan domain.Observation, 1)
		start(func() {
			runBlockTiming(ctx, st, metricsSampler, heads.timing, consensusClient, cfg.CLMetricsAPI, genesis, clk, clockMaxAge, logger)
		})
	}

	if cfg.BaselineBeaconAPI != "" {
		baselineClient := beaconapi.NewClient(cfg.BaselineBeaconAPI, cfg.MinRequestInterval)
		baselineGenesis, err := baselineClient.FetchGenesis(ctx)
		if err != nil {
			return fmt.Errorf("fetch baseline genesis: %w", err)
		}
		if err := validateBaselineGenesis(genesis, baselineGenesis); err != nil {
			return err
		}
		// Client detection is only needed to pick a Prometheus adapter. The
		// Beacon API path reads the baseline node's own
		// /eth/v1/beacon/headers/{slot}, which every client serves identically,
		// so demanding a recognised client there would reject a perfectly usable
		// baseline for no reason (ADR-0025).
		var baselineConsensus source.ConsensusClient
		if cfg.BaselineMetricsAPI != "" {
			versionString, err := baselineClient.FetchNodeVersion(ctx)
			if err != nil {
				return fmt.Errorf("fetch baseline node version: %w", err)
			}
			baselineConsensus = source.DetectConsensusClient(versionString)
			if baselineConsensus == source.ConsensusUnknown {
				return fmt.Errorf("network baseline metrics are not supported for consensus client version %q; unset --baseline-metrics-api to use the Beacon API instead", versionString)
			}
			logger.Info("detected baseline consensus client", "version", versionString, "client", baselineConsensus)
		} else {
			logger.Info("network baseline reads the independent node's Beacon API", "baseline_beacon_api", cfg.BaselineBeaconAPI)
		}
		// Only the metrics path is head-driven; the Beacon API path runs off the
		// slot clock so its measurement is not shifted by however late this node
		// saw the block. See runNetworkBaselineFromAPI.
		if cfg.BaselineMetricsAPI != "" {
			heads.baseline = make(chan domain.Observation, 1)
			start(func() {
				runNetworkBaseline(ctx, st, metricsSampler, heads.baseline, baselineConsensus, cfg.BaselineMetricsAPI, genesis, clk, clockMaxAge, logger)
			})
		} else {
			start(func() {
				runNetworkBaselineFromAPI(ctx, st, baselineClient, genesis, clk, clockMaxAge, logger)
			})
		}
	}
	start(func() { runSlotClock(ctx, st, genesis, clk, clockMaxAge, logger) })

	if len(cfg.ValidatorIndices) > 0 {
		exp := exporter.New()
		start(func() {
			runDutyTracking(ctx, st, client, cfg.ValidatorIndices, cfg.DBPath, schedule, rcaConfig, exp, genesis, &heads, clk, clockMaxAge, logger)
		})

		if cfg.MetricsAddr != "" {
			var listenConfig net.ListenConfig
			listener, err := listenConfig.Listen(ctx, "tcp", cfg.MetricsAddr)
			if err != nil {
				return fmt.Errorf("listen for metrics on %s: %w", cfg.MetricsAddr, err)
			}
			listener, err = newBoundedListener(listener, maxMetricsConnections)
			if err != nil {
				return fmt.Errorf("bound metrics listener: %w", err)
			}
			mux := http.NewServeMux()
			mux.Handle("/metrics", exp.Handler())
			srv := &http.Server{
				Addr: cfg.MetricsAddr, Handler: mux,
				ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
				WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
				MaxHeaderBytes: 16 << 10,
			}
			start(func() {
				<-ctx.Done()
				// Preserve request-scoped values but remove the cancellation
				// that triggered shutdown, then apply a fresh graceful deadline.
				shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				if err := srv.Shutdown(shutdownCtx); err != nil {
					logger.Error("shut down metrics server", "error", err)
				}
			})
			start(func() {
				logger.Info("serving metrics", "addr", cfg.MetricsAddr)
				if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					serveErr := fmt.Errorf("serve metrics on %s: %w", cfg.MetricsAddr, err)
					select {
					case fatalErrors <- serveErr:
					default:
						logger.Error("metrics server", "error", serveErr)
					}
				}
			})
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-fatalErrors:
			return err
		case obs, ok := <-events:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("beacon event stream stopped unexpectedly")
			}
			// An observation in hand is the only proof the stream works; the
			// reconnect loop cannot tell a connection that succeeded from one
			// that is about to fail.
			streamState.recovered(logger)
			trusted := stampClock(clk, clockMaxAge, obs)
			if err := st.WriteObservation(ctx, trusted); err != nil {
				logger.Error("write observation", "error", err, "kind", obs.Kind, "slot", obs.Slot)
			}
			heads.send(trusted, logger)
		}
	}
}

// runSlotClock writes a derived ObsSlotStart observation for every slot as
// it begins.
//
// Found missing by actually running whymiss watch against a live devnet
// (BUILD_PROMPT.md §10.3's real end-to-end check, not just unit tests
// against hand-assembled observation slices that always happened to
// already include one): the SSE stream only carries head/chain_reorg
// events, and nothing else in this loop ever produced slot_start —
// GetTimeline requires exactly one, so `whymiss timeline` failed for every
// slot watch had actually collected. slot_start is Source: SourceDerived,
// not SourceBeaconAPI, because it is computed from genesis + the slot
// schedule, the same as tools/faultinjector's own slot_start observations.
func runSlotClock(ctx context.Context, st *store.Store, genesis beaconapi.GenesisInfo, clk *clock.Tracker, clockMaxAge time.Duration, logger *slog.Logger) {
	for {
		now := time.Now().UTC()
		untilGenesis := genesis.GenesisTime.Sub(now)
		if untilGenesis > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(untilGenesis):
			}
			continue
		}

		currentSlot := domain.Slot(now.Sub(genesis.GenesisTime) / genesis.SecondsPerSlot) //nolint:gosec // G115: guarded by untilGenesis <= 0 above, so the duration here is never negative
		slotStart := genesis.SlotStart(uint64(currentSlot))

		obs, err := domain.NewObservation(domain.Observation{
			Slot: currentSlot, Kind: domain.ObsSlotStart, At: slotStart, Source: domain.SourceDerived,
		})
		if err != nil {
			logger.Error("build slot_start observation", "error", err, "slot", currentSlot)
		} else if err := st.WriteObservation(ctx, stampClock(clk, clockMaxAge, obs)); err != nil {
			logger.Error("write slot_start", "error", err, "slot", currentSlot)
		}

		nextSlotStart := genesis.SlotStart(uint64(currentSlot) + 1)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(nextSlotStart)):
		}
	}
}

// runPeerSampling periodically reads the watched node's connected peer count
// from the standardised Beacon API endpoint rather than from either client's
// Prometheus surface. beaconapi.Client.PeerCount's doc comment records why:
// Lighthouse's libp2p_peers gauge reads 0 on a genuinely peered node, which
// made R-200's peer corroboration vacuous there. One spec-defined endpoint also
// means no client-specific code for this fact at all, which is where I-11
// points.
func runPeerSampling(ctx context.Context, st *store.Store, client *beaconapi.Client, interval time.Duration, clk *clock.Tracker, clockMaxAge time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample, err := client.PeerCount(ctx)
			if err != nil {
				logger.Debug("sample peer count unavailable", "error", err)
				continue
			}
			if err := st.WriteSample(ctx, stampSampleClock(clk, clockMaxAge, sample)); err != nil {
				logger.Error("write sample", "error", err)
			}
		}
	}
}

func runRetention(ctx context.Context, st *store.Store, cfg WatchConfig, logger *slog.Logger) {
	if err := st.Prune(ctx, cfg.RetentionMaxAge, cfg.RetentionMaxBytes); err != nil && ctx.Err() == nil {
		logger.Error("initial prune", "error", err)
	}
	ticker := time.NewTicker(cfg.RetentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := st.Prune(ctx, cfg.RetentionMaxAge, cfg.RetentionMaxBytes); err != nil {
				logger.Error("prune", "error", err)
			}
		}
	}
}

func runHostSampling(ctx context.Context, st *store.Store, interval time.Duration, clk *clock.Tracker, clockMaxAge time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var cpu hostmetrics.CPUSteal
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if sample, err := hostmetrics.SampleIOPressure(); err != nil {
				logger.Debug("sample io pressure unavailable", "error", err)
			} else if err := st.WriteSample(ctx, stampSampleClock(clk, clockMaxAge, sample)); err != nil {
				logger.Error("write sample", "error", err)
			}
			if sample, err := hostmetrics.SampleMemoryPressure(); err != nil {
				logger.Debug("sample memory pressure unavailable", "error", err)
			} else if err := st.WriteSample(ctx, stampSampleClock(clk, clockMaxAge, sample)); err != nil {
				logger.Error("write sample", "error", err)
			}
			if sample, ok, err := cpu.Sample(); err != nil {
				logger.Debug("sample cpu steal unavailable", "error", err)
			} else if ok {
				if err := st.WriteSample(ctx, stampSampleClock(clk, clockMaxAge, sample)); err != nil {
					logger.Error("write sample", "error", err)
				}
			}
		}
	}
}
