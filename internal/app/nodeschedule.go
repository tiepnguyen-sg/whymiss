package app

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
)

// adoptNodeSchedule replaces the default slot schedule with the one the node
// reports, and leaves an operator's own schedule alone.
//
// The node is the better source for the same reason peer count and the network
// baseline moved to the Beacon API (ADR-0023, ADR-0025): it is a fact the
// standardised API already publishes, and asking an operator to retype it is a
// way to be wrong. A fork that moves the timing model then needs no action at
// all, which is what BUILD_PROMPT task 5.4 is for.
//
// "Left alone" is decided by comparing against domain.MainnetPreEPBS(). An
// operator who set the schedule to something else meant it, and a disagreement
// with the node is theirs to resolve; an operator who set it to exactly the
// mainnet defaults is indistinguishable from one who set nothing, and gets the
// same values back from any mainnet node anyway.
//
// Failure is never fatal. A node that does not publish the keys, or answers with
// something unusable, leaves the schedule exactly as configured — the daemon
// still runs, with the timing model it already had.
func adoptNodeSchedule(
	ctx context.Context,
	client *beaconapi.Client,
	genesis beaconapi.GenesisInfo,
	configured domain.SlotSchedule,
	logger *slog.Logger,
) domain.SlotSchedule {
	if configured != domain.MainnetPreEPBS() {
		logger.Info("slot schedule comes from configuration, not from the node",
			"seconds_per_slot", configured.SecondsPerSlot.String(),
			"attestation_deadline", configured.AttestationDeadline.String())
		return configured
	}

	headEpoch := currentEpoch(genesis, time.Now().UTC())
	fetched, ok, err := client.FetchSchedule(ctx, headEpoch)
	switch {
	case err != nil:
		logger.Warn("could not read the slot schedule from the node; keeping the configured one",
			"error", err, "attestation_deadline", configured.AttestationDeadline.String())
		return configured
	case !ok:
		logger.Info("node does not publish its slot timing; keeping the configured schedule",
			"attestation_deadline", configured.AttestationDeadline.String())
		return configured
	case fetched == configured:
		return configured
	}

	logger.Info("slot schedule adopted from the node's own spec",
		"seconds_per_slot", fetched.SecondsPerSlot.String(),
		"attestation_deadline", fetched.AttestationDeadline.String(),
		"aggregation_deadline", fetched.AggregationDeadline.String(),
		"payload_reveal_deadline", fetched.PayloadRevealDeadline.String(),
		"ptc_deadline", fetched.PTCDeadline.String(),
		"post_epbs", fetched.IsPostEPBS())
	return fetched
}

// currentEpoch is the epoch the chain is in now, derived from genesis rather
// than fetched: one fewer request, and the answer only has to be good enough to
// tell "before the fork" from "after it".
func currentEpoch(genesis beaconapi.GenesisInfo, now time.Time) uint64 {
	if genesis.SecondsPerSlot <= 0 || now.Before(genesis.GenesisTime) {
		return 0
	}
	slot := uint64(now.Sub(genesis.GenesisTime) / genesis.SecondsPerSlot) //nolint:gosec // guarded above
	return slot / domain.SlotsPerEpoch
}

// ResolveSchedule is adoptNodeSchedule for the one-shot commands.
//
// `whymiss <slot>` and `whymiss timeline` read a database the daemon filled, and
// must reach the same verdict the daemon would. Without this they analyse a
// post-ePBS chain against pre-ePBS deadlines — the same slot explained two ways
// depending on which command asked, which is worse than either answer alone.
//
// A node that is unset, unreachable, or silent leaves the configured schedule in
// place: these commands still work against a database with no node behind it,
// which is how an operator reads a post-mortem after the fact.
func ResolveSchedule(
	ctx context.Context,
	beaconAPI string,
	minRequestInterval time.Duration,
	configured domain.SlotSchedule,
	logger *slog.Logger,
) domain.SlotSchedule {
	if beaconAPI == "" || configured != domain.MainnetPreEPBS() {
		return configured
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	client := beaconapi.NewClient(beaconAPI, minRequestInterval)
	genesis, err := client.FetchGenesis(ctx)
	if err != nil {
		logger.Debug("could not reach the node for its slot schedule; using the configured one", "error", err)
		return configured
	}
	return adoptNodeSchedule(ctx, client, genesis, configured, logger)
}
