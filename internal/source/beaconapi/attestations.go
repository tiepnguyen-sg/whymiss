package beaconapi

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// poolAttestation is the subset of the attestation-pool response this
// package reads. Same Electra-style, single-committee-per-attestation shape
// as blockAttestation — see that type's doc comment for the scope this
// implies.
type poolAttestation struct {
	AggregationBits string `json:"aggregation_bits"`
	Data            struct {
		Slot  string `json:"slot"`
		Index string `json:"index"`
	} `json:"data"`
}

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
		ok, err := c.poolIncludesAttestation(ctx, d)
		if err != nil {
			return domain.Observation{}, false, err
		}
		if ok {
			obs, err := domain.NewObservation(domain.Observation{
				Slot:   d.Slot,
				Kind:   domain.ObsAttestationPublished,
				At:     time.Now().UTC(),
				Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{
					domain.AttrValidatorIndex: strconv.FormatUint(uint64(d.ValidatorIndex), 10),
				},
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

func (c *Client) poolIncludesAttestation(ctx context.Context, d AttesterDuty) (bool, error) {
	var atts []poolAttestation
	found, err := c.get(ctx, fmt.Sprintf("/eth/v1/beacon/pool/attestations?slot=%d", d.Slot), &atts)
	if err != nil {
		return false, fmt.Errorf("fetch attestation pool for slot %d: %w", d.Slot, err)
	}
	if !found {
		return false, nil
	}

	wantSlotStr := strconv.FormatUint(uint64(d.Slot), 10)
	wantIndexStr := strconv.FormatUint(uint64(d.CommitteeIndex), 10)
	for _, att := range atts {
		if att.Data.Slot != wantSlotStr || att.Data.Index != wantIndexStr {
			continue
		}
		included, err := bitSet(att.AggregationBits, d.ValidatorCommitteeIndex)
		if err != nil {
			return false, fmt.Errorf("decode aggregation_bits for slot %d: %w", d.Slot, err)
		}
		if included {
			return true, nil
		}
	}
	return false, nil
}
