package app

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/exporter"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

// watchDeadlineSlots leaves bounded slack for a skipped final inclusion slot to
// become provable after the canonical head advances.
const (
	watchDeadlineSlots = 3
	dutyFetchRetry     = 5 * time.Second

	// inclusionPollSlack is extra time beyond domain's own required minimum
	// (Slot.CollectionWindowEnd) given to CheckInclusion's poll loop, so a
	// slow-but-healthy poll isn't cut off right at the instant validation
	// would first accept a completion marker.
	inclusionPollSlack = 2
)

func collectionWindowEnd(slot domain.Slot, slotStart time.Time, slotDuration time.Duration) time.Time {
	return slot.CollectionWindowEnd(slotStart, slotDuration).Add(inclusionPollSlack * slotDuration)
}

// runDutyTracking fetches validatorIndices' attester duties once per epoch and
// spawns trackDuty for each one returned. A failed fetch is retried until the next
// epoch starts so a transient Beacon API failure does not silently lose every duty
// in the epoch.
func runDutyTracking(ctx context.Context, st *store.Store, client *beaconapi.Client, validatorIndices []domain.ValidatorIndex, dbPath string, schedule domain.SlotSchedule, rcaConfig rca.Config, exp *exporter.Exporter, genesis beaconapi.GenesisInfo, heads *headFanout, clk *clock.Tracker, clockMaxAge time.Duration, logger *slog.Logger) {
	var active sync.WaitGroup
	defer active.Wait()
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
		epoch := currentSlot.Epoch()

		duties, err := client.FetchAttesterDuties(ctx, epoch, validatorIndices)
		if err != nil {
			logger.Error("fetch attester duties", "error", err, "epoch", epoch)
			nextEpochStart := genesis.SlotStart(uint64((epoch + 1).FirstSlot()))
			retry := min(dutyFetchRetry, time.Until(nextEpochStart))
			if retry <= 0 {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(retry):
			}
			continue
		}
		for _, d := range duties {
			slotStart := genesis.SlotStart(uint64(d.Slot))
			if !dutyWindowOpen(time.Now().UTC(), slotStart, genesis.SecondsPerSlot) {
				logger.Debug("skip duty whose observation window already closed", "slot", d.Slot, "validator_index", d.ValidatorIndex)
				continue
			}
			obs, err := domain.NewObservation(domain.Observation{
				Slot: d.Slot, Kind: domain.ObsDutyAssigned, At: time.Now().UTC(), Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{domain.AttrValidatorIndex: strconv.FormatUint(uint64(d.ValidatorIndex), 10)},
			})
			if err != nil {
				logger.Error("build duty_assigned observation", "error", err, "slot", d.Slot, "validator_index", d.ValidatorIndex)
				continue
			}
			if err := st.WriteObservation(ctx, stampClock(clk, clockMaxAge, obs)); err != nil {
				logger.Error("write duty_assigned", "error", err, "slot", d.Slot)
				continue
			}
			active.Add(1)
			go func(d beaconapi.AttesterDuty) {
				defer active.Done()
				trackDuty(ctx, st, client, d, genesis, dbPath, schedule, rcaConfig, exp, heads, clk, clockMaxAge, logger)
			}(d)
		}

		nextEpochStart := genesis.SlotStart(uint64((epoch + 1).FirstSlot()))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(nextEpochStart)):
		}
	}
}

func dutyWindowOpen(now, slotStart time.Time, slotDuration time.Duration) bool {
	return now.Before(slotStart.Add(watchDeadlineSlots * slotDuration))
}

// trackDuty waits for d's slot to start, polls for its block_seen,
// attestation_published, and attestation_included
// observations, writes whichever are found to st, then runs the completed
// slot through Explain and records the result into exp.
//
// Every poll and write is log-and-continue on error, never a return —
// consistent with every other per-tick goroutine in this file
// (runHostSampling, runPeerSampling, runRetention): one duty's polling
// failure must never take down the collector daemon, and never blocks
// another duty's tracking (each runs in its own goroutine).
func trackDuty(ctx context.Context, st *store.Store, client *beaconapi.Client, d beaconapi.AttesterDuty, genesis beaconapi.GenesisInfo, dbPath string, schedule domain.SlotSchedule, rcaConfig rca.Config, exp *exporter.Exporter, heads *headFanout, clk *clock.Tracker, clockMaxAge time.Duration, logger *slog.Logger) {
	slotStart := genesis.SlotStart(uint64(d.Slot))
	waitUntil(ctx, slotStart)
	if ctx.Err() != nil {
		return
	}

	watchDeadline := slotStart.Add(watchDeadlineSlots * genesis.SecondsPerSlot)
	var collectionFailed atomic.Bool

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		obs, found, err := client.BlockSeen(ctx, d.Slot, watchDeadline)
		if err != nil {
			collectionFailed.Store(true)
			logger.Error("poll block_seen", "error", err, "slot", d.Slot)
			return
		}
		if !found {
			return
		}
		if err := st.WriteObservation(ctx, stampClock(clk, clockMaxAge, obs)); err != nil {
			collectionFailed.Store(true)
			logger.Error("write block_seen", "error", err, "slot", d.Slot)
		}
	}()
	go func() {
		defer wg.Done()
		obs, found, err := client.HeadUpdated(ctx, d.Slot, watchDeadline)
		if err != nil {
			collectionFailed.Store(true)
			logger.Error("poll head_updated", "error", err, "slot", d.Slot)
			return
		}
		if !found {
			return
		}
		trusted := stampClock(clk, clockMaxAge, obs)
		if err := st.WriteObservation(ctx, trusted); err != nil {
			collectionFailed.Store(true)
			logger.Error("write head_updated", "error", err, "slot", d.Slot)
			return
		}
		// This is the only head_updated a node without an event stream ever
		// produces, so it must reach every head-driven collector, not just
		// block timing — see headFanout's doc comment.
		heads.send(trusted, logger)
	}()
	go func() {
		defer wg.Done()
		obs, found, err := client.AttestationPublished(ctx, d, watchDeadline)
		if err != nil {
			collectionFailed.Store(true)
			logger.Error("poll attestation_published", "error", err, "slot", d.Slot)
			return
		}
		if !found {
			return
		}
		if err := st.WriteObservation(ctx, stampClock(clk, clockMaxAge, obs)); err != nil {
			collectionFailed.Store(true)
			logger.Error("write attestation_published", "error", err, "slot", d.Slot)
		}
	}()
	wg.Wait()

	inclusionEndSlot := d.Slot.LastAttestationInclusionSlot()
	collectionDeadline := collectionWindowEnd(d.Slot, slotStart, genesis.SecondsPerSlot)
	obs, found, err := client.CheckInclusion(ctx, d.Slot, d, inclusionEndSlot, collectionDeadline)
	if err != nil {
		collectionFailed.Store(true)
		logger.Error("check inclusion", "error", err, "slot", d.Slot)
	} else if found {
		if err := st.WriteObservation(ctx, stampClock(clk, clockMaxAge, obs)); err != nil {
			collectionFailed.Store(true)
			logger.Error("write attestation_included", "error", err, "slot", d.Slot)
		}
	}

	waitUntil(ctx, collectionDeadline)
	if ctx.Err() != nil {
		return
	}
	if !collectionFailed.Load() {
		completed, err := domain.NewObservation(domain.Observation{
			Slot: d.Slot, Kind: domain.ObsCollectionCompleted, At: time.Now().UTC(), Source: domain.SourceDerived,
			Attrs: map[domain.AttrKey]string{domain.AttrValidatorIndex: strconv.FormatUint(uint64(d.ValidatorIndex), 10)},
		})
		if err != nil {
			logger.Error("build collection_completed", "error", err, "slot", d.Slot)
		} else if err := st.WriteObservation(ctx, stampClock(clk, clockMaxAge, completed)); err != nil {
			logger.Error("write collection_completed", "error", err, "slot", d.Slot)
		}
	}

	v, err := ExplainForValidator(ctx, dbPath, d.Slot, d.ValidatorIndex, schedule, rcaConfig)
	if err != nil {
		logger.Error("explain slot", "error", err, "slot", d.Slot)
		return
	}
	exp.Record(v)
	logger.Info("recorded verdict", "slot", d.Slot, "cause", v.ReportedCause(), "outcome", v.Outcome, "confidence", v.Confidence)
}

// waitUntil blocks until t or ctx is done, whichever comes first —
// mirrors tools/faultinjector/main.go's own helper of the same name and
// behaviour (negative durations, i.e. t already passed, return
// immediately).
func waitUntil(ctx context.Context, t time.Time) {
	d := time.Until(t)
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
