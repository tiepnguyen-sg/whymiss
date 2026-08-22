package timeline

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// Assembler collects observations, samples, and duties as adapters produce
// them and builds a domain.Timeline for a slot on request.
//
// Assembler is not safe for concurrent use — a caller with adapters running
// on separate goroutines (internal/source/beaconapi.Client.Stream, for
// instance) must serialize its Add* calls itself, typically by running the
// Assembler on its own goroutine and feeding it off a channel.
//
// Assembler keeps everything it is given for the lifetime of the process;
// it has no retention policy of its own. Bounding memory (I-12) is
// internal/store's job (task 2.5) once persisted values no longer need to
// stay resident here.
type Assembler struct {
	schedule     domain.SlotSchedule
	duties       map[domain.Slot]domain.Duty
	observations map[domain.Slot][]domain.Observation
	samples      []domain.MetricSample
}

// NewAssembler returns an Assembler that builds timelines against schedule.
func NewAssembler(schedule domain.SlotSchedule) *Assembler {
	return &Assembler{
		schedule:     schedule,
		duties:       make(map[domain.Slot]domain.Duty),
		observations: make(map[domain.Slot][]domain.Observation),
	}
}

// AddObservation files obs under its own Slot field.
func (a *Assembler) AddObservation(obs domain.Observation) {
	a.observations[obs.Slot] = append(a.observations[obs.Slot], obs)
}

// AddSample records a metric sample. Samples are not slot-scoped — see
// domain.MetricSample's doc comment — so every Build call after this one
// sees it, until internal/store's retention (task 2.5) starts pruning what
// this Assembler holds.
func (a *Assembler) AddSample(s domain.MetricSample) {
	a.samples = append(a.samples, s)
}

// SetDuty records slot's duty. A slot has at most one duty this build
// attributes (docs/causes.md §2.1): calling this twice for the same slot
// overwrites, it does not append.
func (a *Assembler) SetDuty(d domain.Duty) {
	a.duties[d.Slot] = d
}

// Build returns the domain.Timeline for slot, whose wall-clock start is
// slotStart. Observations and samples are sorted deterministically —
// primarily by timestamp, with a fixed tie-break for equal timestamps —
// so that assembling the same inputs in a different arrival order (a real
// risk when adapters run on separate goroutines) always produces the same
// Timeline. That determinism is what BUILD_PROMPT.md §10.3 requires of
// replay: replaying a corpus scenario's observations.jsonl must produce a
// byte-identical Timeline on every run.
func (a *Assembler) Build(slot domain.Slot, slotStart time.Time) (domain.Timeline, error) {
	obs := slices.Clone(a.observations[slot])
	sortObservations(obs)

	samples := slices.Clone(a.samples)
	sortSamples(samples)

	var duty *domain.Duty
	if d, ok := a.duties[slot]; ok {
		duty = &d
	}

	tl, err := domain.NewTimeline(domain.Timeline{
		Slot:         slot,
		SlotStart:    slotStart,
		Schedule:     a.schedule,
		Duty:         duty,
		Observations: obs,
		Samples:      samples,
	})
	if err != nil {
		return domain.Timeline{}, fmt.Errorf("build timeline for slot %d: %w", slot, err)
	}
	return tl, nil
}

// sortObservations orders by At, then breaks a tie deterministically by
// Kind, then Source, then a stable encoding of Attrs — never by arrival
// order, which is not deterministic across runs.
func sortObservations(obs []domain.Observation) {
	sort.SliceStable(obs, func(i, j int) bool {
		a, b := obs[i], obs[j]
		if !a.At.Equal(b.At) {
			return a.At.Before(b.At)
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return encodeAttrs(a.Attrs) < encodeAttrs(b.Attrs)
	})
}

// sortSamples orders by At, then Component, then Name — the same
// determinism guarantee as sortObservations, for the same reason.
func sortSamples(samples []domain.MetricSample) {
	sort.SliceStable(samples, func(i, j int) bool {
		a, b := samples[i], samples[j]
		if !a.At.Equal(b.At) {
			return a.At.Before(b.At)
		}
		if a.Component != b.Component {
			return a.Component < b.Component
		}
		return a.Name < b.Name
	})
}

// encodeAttrs produces a stable, sorted-by-key string encoding of an
// attribute map, used only as a deterministic tie-breaker — not a format
// anything else parses.
func encodeAttrs(attrs map[domain.AttrKey]string) string {
	if len(attrs) == 0 {
		return ""
	}
	keys := make([]domain.AttrKey, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b domain.AttrKey) int { return cmp.Compare(a, b) })
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(string(k))
		b.WriteByte('=')
		b.WriteString(attrs[k])
		b.WriteByte(';')
	}
	return b.String()
}
