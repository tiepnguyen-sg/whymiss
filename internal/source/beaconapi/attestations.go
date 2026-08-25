package beaconapi

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// apiAttestation is the subset shared by pool and block responses. Electra's
// committee_bits may concatenate several committees into one aggregation
// bitlist (EIP-7549).
type apiAttestation struct {
	AggregationBits string `json:"aggregation_bits"`
	CommitteeBits   string `json:"committee_bits"`
	Data            struct {
		Slot            string `json:"slot"`
		Index           string `json:"index"`
		BeaconBlockRoot string `json:"beacon_block_root"`
		Target          struct {
			Epoch string `json:"epoch"`
			Root  string `json:"root"`
		} `json:"target"`
	} `json:"data"`
}

type poolAttestation = apiAttestation

// AttestationPublished polls GET /eth/v1/beacon/pool/attestations?slot= every
// 500ms until d's validator's bit appears set in a matching attestation, or
// deadline passes, returning an ObsAttestationPublished observation the
// instant it does.
//
// This is a proxy for "the validator client published its attestation," not
// a direct measurement of it: the pool reflects what this beacon node has
// received and aggregated, which lags true publish time by however long
// gossip propagation and aggregation took. A caller comparing the returned
// observation's timestamp against the attestation deadline should keep that
// slack in mind.
func (c *Client) AttestationPublished(ctx context.Context, d AttesterDuty, deadline time.Time) (domain.Observation, bool, error) {
	const pollInterval = 500 * time.Millisecond
	for {
		ok, match, err := c.poolIncludesAttestation(ctx, d)
		if err != nil {
			// A single failed poll — e.g. a transient HTTP timeout from a
			// node under the exact CPU pressure local.vc_slow exists to
			// diagnose — used to abort this whole call immediately, the same
			// bug HeadUpdated had (see its doc comment). "Not found yet"
			// already tolerates this many times over in the loop below; one
			// failed attempt gets the same tolerance unless ctx itself is
			// why it failed, in which case retrying cannot help.
			if ctx.Err() != nil {
				return domain.Observation{}, false, err
			}
			ok = false
		}
		if ok {
			attrs := map[domain.AttrKey]string{
				domain.AttrValidatorIndex: strconv.FormatUint(uint64(d.ValidatorIndex), 10),
			}
			if match.Data.BeaconBlockRoot != "" {
				attrs[domain.AttrBlockRoot] = match.Data.BeaconBlockRoot
			}
			obs, err := domain.NewObservation(domain.Observation{
				Slot:   d.Slot,
				Kind:   domain.ObsAttestationPublished,
				At:     time.Now().UTC(),
				Source: domain.SourceBeaconAPI,
				Attrs:  attrs,
			})
			if err != nil {
				return domain.Observation{}, false, fmt.Errorf("build attestation_published observation for slot %d: %w", d.Slot, err)
			}
			return obs, true, nil
		}
		if !time.Now().Before(deadline) {
			return domain.Observation{}, false, nil
		}
		select {
		case <-ctx.Done():
			return domain.Observation{}, false, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (c *Client) poolIncludesAttestation(ctx context.Context, d AttesterDuty) (bool, apiAttestation, error) {
	var atts []poolAttestation
	found, err := c.get(ctx, fmt.Sprintf("/eth/v1/beacon/pool/attestations?slot=%d", d.Slot), &atts)
	if err != nil {
		return false, apiAttestation{}, fmt.Errorf("fetch attestation pool for slot %d: %w", d.Slot, err)
	}
	if !found {
		return false, apiAttestation{}, nil
	}

	included, needCommittees, match, err := attestationsIncludeValidator(atts, d.Slot, d, nil)
	if err != nil || included || !needCommittees {
		return included, match, err
	}
	lengths, err := c.committeeLengthsForSlot(ctx, d.Slot, d.CommitteesAtSlot)
	if err != nil {
		return false, apiAttestation{}, fmt.Errorf("fetch committee lengths for slot %d: %w", d.Slot, err)
	}
	included, _, match, err = attestationsIncludeValidator(atts, d.Slot, d, lengths)
	return included, match, err
}
