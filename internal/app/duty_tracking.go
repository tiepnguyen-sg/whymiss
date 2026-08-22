package app

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/exporter"
	"github.com/CHANGEME/whymiss/internal/source/beaconapi"
	"github.com/CHANGEME/whymiss/internal/store"
)

// watchDeadlineSlots and inclusionWindowSlots mirror
// tools/faultinjector/main.go's own margins (that tool's watchDeadline is
// slotStart + 3*SecondsPerSlot, and it checks inclusion up to
// dutySlot+2) — the same real-devnet-verified amount of slack between a
// duty's slot and knowing its outcome.
const (
	watchDeadlineSlots   = 3
	inclusionWindowSlots = 2
)

// runDutyTracking fetches validatorIndices' attester duties once per
// epoch and spawns trackDuty for each one returned. Errors fetching
// duties for one epoch are logged and skipped — the next epoch's fetch is
// an independent attempt, not a retry of this one (matches
// beaconapi.Client.FetchAttesterDuties's own doc comment: only the
// current and next epoch are ever computable, so there is no "try this
// epoch again later" that would help).
func runDutyTracking(ctx context.Context, st *store.Store, client *beaconapi.Client, validatorIndices []domain.ValidatorIndex, dbPath string, schedule domain.SlotSchedule, exp *exporter.Exporter, genesis beaconapi.GenesisInfo, logger *slog.Logger) {
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
		}
		for _, d := range duties {
			obs, err := domain.NewObservation(domain.Observation{
				Slot: d.Slot, Kind: domain.ObsDutyAssigned, At: time.Now().UTC(), Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{domain.AttrValidatorIndex: strconv.FormatUint(uint64(d.ValidatorIndex), 10)},
			})
			if err != nil {
				logger.Error("build duty_assigned observation", "error", err, "slot", d.Slot, "validator_index", d.ValidatorIndex)
				continue
			}
			if err := st.WriteObservation(ctx, obs); err != nil {
				logger.Error("write duty_assigned", "error", err, "slot", d.Slot)
			}
			go trackDuty(ctx, st, client, d, genesis, dbPath, schedule, exp, logger)
		}

		nextEpochStart := genesis.SlotStart(uint64((epoch + 1).FirstSlot()))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(nextEpochStart)):
		}
	}
}

// trackDuty waits for d's slot to start, polls for its block_seen,
// attestation_published, and (if a block was seen) attestation_included
// observations, writes whichever are found to st, then runs the completed
// slot through Explain and records the result into exp.
//
// Every poll and write is log-and-continue on error, never a return —
// consistent with every other per-tick goroutine in this file
// (runHostSampling, runPeerSampling, runRetention): one duty's polling
// failure must never take down the collector daemon, and never blocks
// another duty's tracking (each runs in its own goroutine).
func trackDuty(ctx context.Context, st *store.Store, client *beaconapi.Client, d beaconapi.AttesterDuty, genesis beaconapi.GenesisInfo, dbPath string, schedule domain.SlotSchedule, exp *exporter.Exporter, logger *slog.Logger) {
	slotStart := genesis.SlotStart(uint64(d.Slot))
	waitUntil(ctx, slotStart)
	if ctx.Err() != nil {
		return
	}

	watchDeadline := slotStart.Add(watchDeadlineSlots * genesis.SecondsPerSlot)

	var wg sync.WaitGroup
	var blockFound bool
	wg.Add(2)
	go func() {
		defer wg.Done()
		obs, found, err := client.BlockSeen(ctx, d.Slot, watchDeadline)
		if err != nil {
			logger.Error("poll block_seen", "error", err, "slot", d.Slot)
			return
		}
		if !found {
			return
		}
		blockFound = true
		if err := st.WriteObservation(ctx, obs); err != nil {
			logger.Error("write block_seen", "error", err, "slot", d.Slot)
		}
	}()
	go func() {
		defer wg.Done()
		obs, found, err := client.AttestationPublished(ctx, d, watchDeadline)
		if err != nil {
			logger.Error("poll attestation_published", "error", err, "slot", d.Slot)
			return
		}
		if !found {
			return
		}
		if err := st.WriteObservation(ctx, obs); err != nil {
			logger.Error("write attestation_published", "error", err, "slot", d.Slot)
		}
	}()
	wg.Wait()

	if blockFound {
		obs, found, err := client.CheckInclusion(ctx, d.Slot, d, d.Slot+inclusionWindowSlots, watchDeadline.Add(inclusionWindowSlots*genesis.SecondsPerSlot))
		if err != nil {
			logger.Error("check inclusion", "error", err, "slot", d.Slot)
		} else if found {
			if err := st.WriteObservation(ctx, obs); err != nil {
				logger.Error("write attestation_included", "error", err, "slot", d.Slot)
			}
		}
	}

	v, err := Explain(ctx, dbPath, d.Slot, schedule)
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
