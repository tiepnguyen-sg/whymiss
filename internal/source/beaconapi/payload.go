package beaconapi

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// maxPayloadAttestations bounds what a block may claim, so a malformed or
// hostile response cannot make this allocate without limit (I-12).
const maxPayloadAttestations = 64

// payloadAttestation is one payload-timeliness committee vote as the Beacon API
// serves it inside a block body.
type payloadAttestation struct {
	Data struct {
		Slot           string `json:"slot"`
		PayloadPresent bool   `json:"payload_present"`
	} `json:"data"`
}

// PayloadAttested reports the payload-timeliness committee's verdict for slot,
// read from the block that follows it.
//
// Under EIP-7732 the committee votes on whether the execution payload was
// revealed in time, and the votes are carried in the *next* block's
// payload_attestations — measured on a Glamsterdam devnet 2026-08-30, where the
// block at slot N+1 carries votes whose data.slot is N. This is a standardised
// Beacon API field, so unlike block-arrival timing it needs no client-specific
// adapter (ADR-0027, following ADR-0023 and ADR-0025).
//
// found is false when this fork has no such votes, when the following slot was
// skipped, or when the block carries no vote for this slot. None of those is an
// error: they mean the committee's finding is unavailable, which the rules must
// treat as absence of evidence rather than as evidence of absence (I-8).
func (c *Client) PayloadAttested(ctx context.Context, slot domain.Slot, at time.Time) (domain.Observation, bool, error) {
	votes, found, err := c.fetchPayloadAttestations(ctx, slot+1)
	if err != nil {
		return domain.Observation{}, false, fmt.Errorf("payload attestations for slot %d: %w", slot+1, err)
	}
	if !found || len(votes) == 0 {
		return domain.Observation{}, false, nil
	}

	counted, present := 0, 0
	for _, vote := range votes {
		voteSlot, parseErr := strconv.ParseUint(vote.Data.Slot, 10, 64)
		if parseErr != nil || domain.Slot(voteSlot) != slot {
			continue
		}
		counted++
		if vote.Data.PayloadPresent {
			present++
		}
	}
	if counted == 0 {
		return domain.Observation{}, false, nil
	}

	// The committee is a committee: individual votes may disagree, so the
	// observation records the majority finding and how many votes stood behind
	// it. A rule that wants to be cautious can read ptc_votes and decline on a
	// thin sample rather than trusting a single voter.
	obs, err := domain.NewObservation(domain.Observation{
		Slot:   slot,
		Kind:   domain.ObsPayloadAttested,
		At:     at,
		Source: domain.SourceBeaconAPI,
		Attrs: map[domain.AttrKey]string{
			domain.AttrPayloadPresent: strconv.FormatBool(present*2 > counted),
			domain.AttrPTCVotes:       strconv.Itoa(counted),
		},
	})
	if err != nil {
		return domain.Observation{}, false, fmt.Errorf("build payload_attested for slot %d: %w", slot, err)
	}
	return obs, true, nil
}

func (c *Client) fetchPayloadAttestations(ctx context.Context, slot domain.Slot) ([]payloadAttestation, bool, error) {
	var envelope struct {
		Data struct {
			Message struct {
				Body struct {
					PayloadAttestations []payloadAttestation `json:"payload_attestations"`
				} `json:"body"`
			} `json:"message"`
		} `json:"data"`
	}
	found, err := c.getEnvelope(ctx, fmt.Sprintf("/eth/v2/beacon/blocks/%d", slot), &envelope)
	if err != nil {
		if unsupportedEndpointError(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	votes := envelope.Data.Message.Body.PayloadAttestations
	if len(votes) > maxPayloadAttestations {
		return nil, false, fmt.Errorf("block carries %d payload attestations, limit is %d", len(votes), maxPayloadAttestations)
	}
	return votes, true, nil
}
