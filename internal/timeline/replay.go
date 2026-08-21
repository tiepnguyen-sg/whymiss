package timeline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/CHANGEME/whymiss/internal/domain"
)

// LoadObservations decodes path (test/corpus/<id>/observations.jsonl: one
// JSON-encoded domain.Observation per line, the exact wire form
// tools/faultinjector's WriteCorpusScenario writes) into a slice.
//
// This is what makes a corpus scenario replayable rather than a one-off log
// — the same format the collector will write in Phase 2 proper and this
// reads back in Phase 3's rule tests, so the two never have to be kept in
// sync by hand.
func LoadObservations(path string) ([]domain.Observation, error) {
	f, err := os.Open(path) //nolint:gosec // G304: path is a corpus fixture the caller names explicitly, not attacker-controlled input
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only file; nothing to act on if Close fails

	var out []domain.Observation
	scanner := bufio.NewScanner(f)
	for lineNum := 1; scanner.Scan(); lineNum++ {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var draft domain.Observation
		if err := json.Unmarshal(line, &draft); err != nil {
			return nil, fmt.Errorf("%s:%d: decode observation: %w", path, lineNum, err)
		}
		obs, err := domain.NewObservation(draft)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNum, err)
		}
		out = append(out, obs)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

// Replay assembles a slice of observations (typically loaded with
// [LoadObservations]) back into a domain.Timeline, exactly as a live
// collector run against the same facts would have. Every observation must
// share the same Slot and there must be an ObsSlotStart observation among
// them — both true of every scenario tools/faultinjector generates — since
// there is no other source for either in a replayed file.
//
// A duty is reconstructed from an ObsDutyAssigned observation when one is
// present. Only Kind (always attester — replay's inputs never include a
// proposer-duty corpus scenario, since BUILD_PROMPT §2.1 defers proposer
// duty attribution) and ValidatorIndex are recoverable this way;
// CommitteeIndex is decoding mechanics an observation was never the place
// to carry (see beaconapi.AttesterDuty's doc comment) and is left zero.
func Replay(observations []domain.Observation, schedule domain.SlotSchedule) (domain.Timeline, error) {
	if len(observations) == 0 {
		return domain.Timeline{}, fmt.Errorf("replay: no observations")
	}

	slot := observations[0].Slot
	for i, obs := range observations {
		if obs.Slot != slot {
			return domain.Timeline{}, fmt.Errorf("replay: observation %d is for slot %d, want slot %d (replay does not mix slots)", i, obs.Slot, slot)
		}
	}

	a := NewAssembler(schedule)
	var slotStart domain.Observation
	haveSlotStart := false
	for _, obs := range observations {
		a.AddObservation(obs)
		if obs.Kind == domain.ObsSlotStart {
			slotStart, haveSlotStart = obs, true
		}
		if obs.Kind == domain.ObsDutyAssigned {
			if vi, ok := obs.Attr(domain.AttrValidatorIndex); ok {
				parsed, err := strconv.ParseUint(vi, 10, 64)
				if err != nil {
					return domain.Timeline{}, fmt.Errorf("replay: parse validator_index %q: %w", vi, err)
				}
				a.SetDuty(domain.Duty{Kind: domain.DutyAttester, Slot: slot, ValidatorIndex: domain.ValidatorIndex(parsed)})
			}
		}
	}
	if !haveSlotStart {
		return domain.Timeline{}, fmt.Errorf("replay: no slot_start observation for slot %d", slot)
	}

	tl, err := a.Build(slot, slotStart.At)
	if err != nil {
		return domain.Timeline{}, fmt.Errorf("replay: %w", err)
	}
	return tl, nil
}
