package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/exporter"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
	"github.com/tiepnguyen-sg/whymiss/internal/source/hostmetrics"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

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

	// PeerSampleInterval is how often CLMetricsAPI is scraped for peer
	// count, when CLMetricsAPI is set.
	PeerSampleInterval time.Duration

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
	// domain.MainnetPreEPBS() when zero — the schedule this build ships
	// with until Phase 5's configurable SlotSchedule lands (matches
	// cmd/whymiss/timeline.go's own default).
	Schedule domain.SlotSchedule

	Logger *slog.Logger
}

// Watch runs the collector daemon until ctx is done: it streams the beacon
// node's head/chain_reorg events, optionally samples host resource
// pressure, and persists everything to cfg.DBPath, pruning on a timer.
// When cfg.ValidatorIndices is non-empty, it also tracks each validator's
// attester duties, polls their outcome, runs every completed duty through
// rca.Analyze (via Explain), and records the result into a Prometheus
// exporter — see duty_tracking.go and internal/exporter.
//
// The observation-collector half is task 2.7's minimal real daemon — it
// proves the composition (source adapters -> store) runs end to end
// against a live node. Per-duty tracking (polling a specific validator's
// block_seen/attestation_published/attestation_included) landed later,
// task 4.1, once there was a config surface (--validator-index, plain
// cobra flags rather than the koanf-backed config file BUILD_PROMPT §3
// eventually assigns this to) and a consumer for the result (the
// exporter) to justify it; see CHANGELOG.md.
func Watch(ctx context.Context, cfg WatchConfig) error {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close() //nolint:errcheck // best-effort on shutdown; ctx cancellation already determined the exit path

	client := beaconapi.NewClient(cfg.BeaconAPI, cfg.MinRequestInterval)
	genesis, err := client.FetchGenesis(ctx)
	if err != nil {
		return fmt.Errorf("fetch genesis: %w", err)
	}
	logger.Info("connected to beacon node", "beacon_api", cfg.BeaconAPI, "genesis_time", genesis.GenesisTime)

	events := client.Stream(ctx, func(streamErr error) {
		logger.Warn("event stream error, reconnecting", "error", streamErr)
	})

	if cfg.RetentionInterval > 0 {
		go runRetention(ctx, st, cfg, logger)
	}
	if cfg.HostSampleInterval > 0 {
		go runHostSampling(ctx, st, cfg.HostSampleInterval, logger)
	}
	if cfg.CLMetricsAPI != "" {
		versionString, err := client.FetchNodeVersion(ctx)
		if err != nil {
			return fmt.Errorf("fetch node version: %w", err)
		}
		consensusClient := source.DetectConsensusClient(versionString)
		logger.Info("detected consensus client", "version", versionString, "client", consensusClient)
		go runPeerSampling(ctx, st, consensusClient, cfg.CLMetricsAPI, cfg.PeerSampleInterval, logger)
	}
	go runSlotClock(ctx, st, genesis, logger)

	if len(cfg.ValidatorIndices) > 0 {
		schedule := cfg.Schedule
		if schedule == (domain.SlotSchedule{}) {
			schedule = domain.MainnetPreEPBS()
		}
		exp := exporter.New()
		go runDutyTracking(ctx, st, client, cfg.ValidatorIndices, cfg.DBPath, schedule, exp, genesis, logger)

		if cfg.MetricsAddr != "" {
			mux := http.NewServeMux()
			mux.Handle("/metrics", exp.Handler())
			srv := &http.Server{Addr: cfg.MetricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			//nolint:gosec // G118: context.Background() below is deliberate, see comment there — ctx is already cancelled by the time this goroutine unblocks
			go func() {
				<-ctx.Done()
				// context.Background() is deliberate, not an oversight: ctx
				// is already cancelled at this point (that's what unblocked
				// this goroutine), so it cannot also bound Shutdown's own
				// timeout — a cancelled context makes Shutdown return
				// immediately without waiting for in-flight requests, the
				// opposite of "graceful."
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx) //nolint:errcheck,contextcheck // best-effort on shutdown, matching st.Close() above; contextcheck's "non-inherited context" warning is the same false positive gosec's G118 flags above
			}()
			go func() {
				logger.Info("serving metrics", "addr", cfg.MetricsAddr)
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Error("metrics server", "error", err)
				}
			}()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case obs, ok := <-events:
			if !ok {
				return nil
			}
			if err := st.WriteObservation(ctx, obs); err != nil {
				logger.Error("write observation", "error", err, "kind", obs.Kind, "slot", obs.Slot)
			}
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
func runSlotClock(ctx context.Context, st *store.Store, genesis beaconapi.GenesisInfo, logger *slog.Logger) {
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
		} else if err := st.WriteObservation(ctx, obs); err != nil {
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

// runPeerSampling periodically scrapes consensusClient's peer count via
// source.SamplePeerCount — the dispatcher, not a client-named function —
// so this file itself never needs to know which client it's talking to.
// See internal/source/peers.go's doc comment: adding a third client means
// a new case there and a new function in internal/source/promscrape,
// nothing here.
func runPeerSampling(ctx context.Context, st *store.Store, consensusClient source.ConsensusClient, metricsURL string, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample, err := source.SamplePeerCount(ctx, consensusClient, metricsURL)
			if err != nil {
				logger.Debug("sample peer count unavailable", "error", err)
				continue
			}
			if err := st.WriteSample(ctx, sample); err != nil {
				logger.Error("write sample", "error", err)
			}
		}
	}
}

func runRetention(ctx context.Context, st *store.Store, cfg WatchConfig, logger *slog.Logger) {
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

func runHostSampling(ctx context.Context, st *store.Store, interval time.Duration, logger *slog.Logger) {
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
			} else if err := st.WriteSample(ctx, sample); err != nil {
				logger.Error("write sample", "error", err)
			}
			if sample, err := hostmetrics.SampleMemoryPressure(); err != nil {
				logger.Debug("sample memory pressure unavailable", "error", err)
			} else if err := st.WriteSample(ctx, sample); err != nil {
				logger.Error("write sample", "error", err)
			}
			if sample, ok, err := cpu.Sample(); err != nil {
				logger.Debug("sample cpu steal unavailable", "error", err)
			} else if ok {
				if err := st.WriteSample(ctx, sample); err != nil {
					logger.Error("write sample", "error", err)
				}
			}
		}
	}
}
