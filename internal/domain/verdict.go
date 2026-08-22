package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Outcome is what actually happened to the duty.
//
// The four-value form is deliberate and refines the three-value enum sketched in
// BUILD_PROMPT §6: that version cannot express partial reward loss, which is the
// most common real-world case and is invisible in most existing tooling
// (docs/causes.md §2).
type Outcome string

const (
	// OutcomeNoDuty means nothing was owed this slot. Not a failure, and reporting
	// it as one destroys trust faster than missing a real cause.
	OutcomeNoDuty Outcome = "no_duty"

	// OutcomeOK means the duty was fulfilled and every reward flag was earned.
	OutcomeOK Outcome = "ok"

	// OutcomeDegraded means the attestation was included on chain but one or more
	// reward flags were lost. This is the signal an operator sees most often.
	OutcomeDegraded Outcome = "degraded"

	// OutcomeMissed means the duty never made it on chain.
	OutcomeMissed Outcome = "missed"
)

// Valid reports whether the outcome is in the closed set.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeNoDuty, OutcomeOK, OutcomeDegraded, OutcomeMissed:
		return true
	default:
		return false
	}
}

// RewardFlags mirrors the consensus-spec participation flags. Which flags were lost
// is what separates a degraded attestation from a clean one, and points at how late
// it was.
type RewardFlags struct {
	// TimelySource is earned for the correct source checkpoint with inclusion delay
	// inside the bound.
	TimelySource bool `json:"timely_source"`

	// TimelyTarget is earned for the correct target checkpoint.
	TimelyTarget bool `json:"timely_target"`

	// TimelyHead is earned for the correct head root with an inclusion delay of
	// exactly one slot. It is the flag that is lost first and noticed last.
	TimelyHead bool `json:"timely_head"`
}

// AllEarned reports whether every flag was earned.
func (r RewardFlags) AllEarned() bool {
	return r.TimelySource && r.TimelyTarget && r.TimelyHead
}

// AnyEarned reports whether at least one flag was earned.
func (r RewardFlags) AnyEarned() bool {
	return r.TimelySource || r.TimelyTarget || r.TimelyHead
}

// Lost returns the names of the flags that were not earned, in spec order. The names
// match the JSON field names, so a report and a machine-readable verdict agree.
func (r RewardFlags) Lost() []string {
	var lost []string
	if !r.TimelySource {
		lost = append(lost, "timely_source")
	}
	if !r.TimelyTarget {
		lost = append(lost, "timely_target")
	}
	if !r.TimelyHead {
		lost = append(lost, "timely_head")
	}
	return lost
}

// Confidence is how much the engine is willing to stand behind a verdict. It is
// computed from dominance, corroboration, and clock trust — never assigned by feel
// (docs/causes.md §4).
type Confidence string

const (
	// ConfidenceHigh is a promise. No high-confidence verdict may be wrong on any
	// corpus scenario, and a single violation blocks a release (I-8).
	ConfidenceHigh Confidence = "high"

	// ConfidenceMedium means one stage clearly dominated but no independent signal
	// corroborated it.
	ConfidenceMedium Confidence = "medium"

	// ConfidenceLow means the evidence points somewhere without being conclusive.
	ConfidenceLow Confidence = "low"
)

// Valid reports whether the confidence is in the closed set.
func (c Confidence) Valid() bool {
	switch c {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
		return true
	default:
		return false
	}
}

// CauseID names a cause in the taxonomy. These strings are a public contract: they
// appear in Prometheus labels, in JSON consumed by operator scripts, and in incident
// threads. A cause ID never changes meaning (I-10, ADR-0005).
type CauseID string

const (
	// CauseProposerMissed means nobody proposed a block for this slot. An
	// exoneration, and a valuable output in its own right.
	CauseProposerMissed CauseID = "network.proposer_missed"

	// CauseLateBlock means the block was late for the network as a whole.
	CauseLateBlock CauseID = "network.late_block"

	// CauseInclusionFailure means the attestation was published on time and never
	// appeared on chain.
	CauseInclusionFailure CauseID = "network.inclusion_failure"

	// CauseP2PDegraded means propagation to this node was slow because peering was
	// insufficient.
	CauseP2PDegraded CauseID = "local.p2p_degraded"

	// CauseCLSlow means the consensus client consumed the validation budget and the
	// execution client is not responsible.
	CauseCLSlow CauseID = "local.cl_slow"

	// CauseELSlow means the execution client responded slowly to Engine API calls.
	CauseELSlow CauseID = "local.el_slow"

	// CauseELSlowSnapshot means the execution client was generating state snapshots.
	CauseELSlowSnapshot CauseID = "local.el_slow.snapshot"

	// CauseELSlowPruning means the execution client was pruning.
	CauseELSlowPruning CauseID = "local.el_slow.pruning"

	// CauseELSlowDiskSaturation means host disk saturation held up the execution
	// client. The most common cause of chronic attestation loss in practice.
	CauseELSlowDiskSaturation CauseID = "local.el_slow.disk_saturation"

	// CauseELSlowSyncing means the execution client was not fully synced.
	CauseELSlowSyncing CauseID = "local.el_slow.syncing"

	// CauseVCSlow means the validator client published after the deadline despite
	// receiving a valid head in time.
	CauseVCSlow CauseID = "local.vc_slow"

	// CauseVCDisconnected means the validator client could not reach the beacon
	// node. Ongoing loss, and worth an alert rather than a post-mortem.
	CauseVCDisconnected CauseID = "local.vc_disconnected"

	// CauseHostDiskIO means host disk I/O pressure was the dominant explanation and
	// no higher-layer cause matched.
	CauseHostDiskIO CauseID = "local.host.disk_io"

	// CauseHostCPUSteal means the hypervisor withheld CPU from this guest.
	CauseHostCPUSteal CauseID = "local.host.cpu_steal"

	// CauseHostMemoryPressure means memory pressure or swap activity delayed
	// processing.
	CauseHostMemoryPressure CauseID = "local.host.memory_pressure"

	// CauseHostClockDrift means the system clock was offset far enough to distort
	// duty timing, which invalidates every other timing measurement (I-9).
	CauseHostClockDrift CauseID = "local.host.clock_drift"

	// CauseInsufficientData means required observations were unavailable, so no
	// honest attribution is possible. Preferable to a guess (I-8).
	CauseInsufficientData CauseID = "unknown.insufficient_data"

	// CauseNoRuleMatched means the data was complete and trustworthy yet no rule
	// matched. A taxonomy gap and a project bug, not an operator problem.
	CauseNoRuleMatched CauseID = "unknown.no_rule_matched"
)

// causeIDs is the closed set, in the order docs/causes.md §7 documents it.
var causeIDs = []CauseID{
	CauseProposerMissed,
	CauseLateBlock,
	CauseInclusionFailure,
	CauseP2PDegraded,
	CauseCLSlow,
	CauseELSlow,
	CauseELSlowSyncing,
	CauseELSlowSnapshot,
	CauseELSlowPruning,
	CauseELSlowDiskSaturation,
	CauseVCDisconnected,
	CauseVCSlow,
	CauseHostDiskIO,
	CauseHostCPUSteal,
	CauseHostMemoryPressure,
	CauseHostClockDrift,
	CauseInsufficientData,
	CauseNoRuleMatched,
}

// CauseIDs returns every cause in the taxonomy, in documentation order. The returned
// slice is a copy.
func CauseIDs() []CauseID {
	out := make([]CauseID, len(causeIDs))
	copy(out, causeIDs)
	return out
}

// Valid reports whether the cause is in the taxonomy.
func (c CauseID) Valid() bool {
	return slices.Contains(causeIDs, c)
}

// IsSubCauseOf reports whether c is a documented sub-cause of parent.
//
// The hierarchy is load-bearing rather than cosmetic: a consumer aggregating on
// local.el_slow must stay correct when a sub-cause is emitted, so a sub-cause may
// never describe a situation its parent would not cover (ADR-0005).
func (c CauseID) IsSubCauseOf(parent CauseID) bool {
	if c == parent || parent == "" {
		return false
	}
	return strings.HasPrefix(string(c), string(parent)+".")
}

// Unit is the unit of a comparison, so a renderer can format a number without
// guessing what it means.
type Unit string

const (
	// UnitMilliseconds is a duration in milliseconds.
	UnitMilliseconds Unit = "ms"

	// UnitPercent is a percentage from 0 to 100.
	UnitPercent Unit = "pct"

	// UnitCount is a dimensionless count, such as peers.
	UnitCount Unit = "count"

	// UnitRatio is a dimensionless ratio from 0 to 1, such as a stage share.
	UnitRatio Unit = "ratio"
)

// Valid reports whether the unit is one a renderer knows how to format.
func (u Unit) Valid() bool {
	switch u {
	case UnitMilliseconds, UnitPercent, UnitCount, UnitRatio:
		return true
	default:
		return false
	}
}

// Comparison is an observed value set against what was expected. It is what turns a
// statement into evidence: "the payload call took 2.8s" is an assertion, "2.8s
// against a rolling p99 of 240ms" is an argument.
type Comparison struct {
	// Label names what was measured, in prose a human reads mid-sentence.
	Label string `json:"label"`

	Observed float64 `json:"observed"`

	// Expected is the baseline, threshold, or budget Observed is judged against.
	Expected float64 `json:"expected"`

	Unit Unit `json:"unit"`
}

// Validate reports why the comparison is not well formed, or nil.
func (c Comparison) Validate() error {
	if strings.TrimSpace(c.Label) == "" {
		return fmt.Errorf("comparison: %w", ErrEmptyStatement)
	}
	if !c.Unit.Valid() {
		return fmt.Errorf("comparison %q: unknown unit %q", c.Label, c.Unit)
	}
	return nil
}

// Evidence is one justification a human can read and a machine can check. No verdict
// exists without at least one (I-7).
type Evidence struct {
	// At is when the fact occurred, in UTC.
	At time.Time `json:"at"`

	// Offset is At relative to the slot start. This is the number an operator reads:
	// "+3.9s" is legible where an absolute timestamp is not.
	Offset time.Duration `json:"offset"`

	// Statement is one line, present tense, no speculation. If it needs a hedge, it
	// is not evidence.
	Statement string `json:"statement"`

	Source SourceID `json:"source"`

	// Comparison is the observed-against-expected pair backing the statement, where
	// one applies.
	Comparison *Comparison `json:"comparison,omitempty"`
}

// Validate reports why the evidence is not well formed, or nil.
func (e Evidence) Validate() error {
	if strings.TrimSpace(e.Statement) == "" {
		return ErrEmptyStatement
	}
	if e.At.IsZero() {
		return fmt.Errorf("%w: evidence %q", ErrMissingTimestamp, e.Statement)
	}
	if e.At.Location() != time.UTC {
		return fmt.Errorf("%w: evidence %q at %s", ErrNotUTC, e.Statement, e.At.Location())
	}
	if !e.Source.Valid() {
		return fmt.Errorf("%w: evidence %q from %q", ErrMissingSource, e.Statement, e.Source)
	}
	if e.Comparison != nil {
		if err := e.Comparison.Validate(); err != nil {
			return fmt.Errorf("evidence %q: %w", e.Statement, err)
		}
	}
	return nil
}

// Verdict is the product's output: what happened, why, how sure whymiss is, and the
// evidence for it.
//
// Build one with NewVerdict. Construction fails on empty evidence, which makes an
// honest unknown the path of least resistance rather than a discipline (I-7, I-8).
type Verdict struct {
	Slot    Slot    `json:"slot"`
	Outcome Outcome `json:"outcome"`

	// Flags records which reward flags were earned. Nil where flags do not apply: a
	// proposer duty, or no duty at all.
	Flags *RewardFlags `json:"flags,omitempty"`

	// Cause is empty when Outcome is OutcomeNoDuty — the taxonomy has no cause ID
	// for "nothing was owed", and inventing one is forbidden.
	Cause CauseID `json:"cause,omitempty"`

	// SubCause refines Cause and must be a documented descendant of it. Empty when
	// no sub-cause matched.
	SubCause CauseID `json:"sub_cause,omitempty"`

	Confidence Confidence `json:"confidence"`
	Evidence   []Evidence `json:"evidence"`

	// Remediation is what the operator should do, in order. Specific commands and
	// settings, never "check your disk". May be empty when the operator did nothing
	// wrong, which is itself worth saying.
	Remediation []string `json:"remediation,omitempty"`

	// EngineVersion and TaxonomyVersion make a stored report self-describing: it can
	// be interpreted years later against the contract it was produced under (I-10).
	EngineVersion   string `json:"engine_version"`
	TaxonomyVersion string `json:"taxonomy_version"`
}

// NewVerdict validates a draft verdict and returns a canonical copy.
//
// TaxonomyVersion is stamped from [TaxonomyVersion] when the draft leaves it empty,
// so no caller can emit a verdict that does not say which taxonomy it speaks. The
// returned value owns its slices.
func NewVerdict(draft Verdict) (Verdict, error) {
	out := draft
	if out.TaxonomyVersion == "" {
		out.TaxonomyVersion = TaxonomyVersion
	}
	if err := out.Validate(); err != nil {
		return Verdict{}, err
	}
	out.Evidence = slices.Clone(draft.Evidence)
	out.Remediation = slices.Clone(draft.Remediation)
	if draft.Flags != nil {
		flags := *draft.Flags
		out.Flags = &flags
	}
	return out, nil
}

// Validate reports why the verdict may not be emitted, or nil.
func (v Verdict) Validate() error {
	if !v.Outcome.Valid() {
		return fmt.Errorf("%w: outcome %q", ErrInvalidOutcome, v.Outcome)
	}
	if err := v.validateCause(); err != nil {
		return err
	}
	if err := v.validateFlags(); err != nil {
		return err
	}
	if !v.Confidence.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidConfidence, v.Confidence)
	}
	if len(v.Evidence) == 0 {
		return fmt.Errorf("%w: slot %d, cause %q", ErrNoEvidence, v.Slot, v.Cause)
	}
	for i, ev := range v.Evidence {
		if err := ev.Validate(); err != nil {
			return fmt.Errorf("evidence %d: %w", i, err)
		}
	}
	if v.EngineVersion == "" {
		return fmt.Errorf("%w: engine version, slot %d", ErrMissingVersion, v.Slot)
	}
	if v.TaxonomyVersion == "" {
		return fmt.Errorf("%w: taxonomy version, slot %d", ErrMissingVersion, v.Slot)
	}
	return nil
}

// validateCause enforces the taxonomy membership and hierarchy rules.
func (v Verdict) validateCause() error {
	if v.Outcome == OutcomeNoDuty {
		if v.Cause != "" {
			return fmt.Errorf("%w: outcome no_duty carries cause %q", ErrInvalidCause, v.Cause)
		}
		if v.SubCause != "" {
			return fmt.Errorf("%w: outcome no_duty carries sub-cause %q", ErrInvalidCause, v.SubCause)
		}
		return nil
	}
	// An empty cause is also legal for OutcomeOK: the duty was fulfilled and
	// no rule found anything wrong, so there is nothing to attribute. Unlike
	// no_duty this is permitted, not required — a rule may legitimately match
	// on a duty that still ended up ok (a validator that was measurably slow
	// yet beat the deadline), and erasing that signal would hide it.
	if v.Outcome == OutcomeOK && v.Cause == "" {
		if v.SubCause != "" {
			return fmt.Errorf("%w: sub-cause %q without a cause", ErrInvalidCause, v.SubCause)
		}
		return nil
	}
	if !v.Cause.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidCause, v.Cause)
	}
	if v.SubCause == "" {
		return nil
	}
	if !v.SubCause.Valid() {
		return fmt.Errorf("%w: sub-cause %q", ErrInvalidCause, v.SubCause)
	}
	if !v.SubCause.IsSubCauseOf(v.Cause) {
		return fmt.Errorf("%w: %q is not a sub-cause of %q", ErrInvalidCause, v.SubCause, v.Cause)
	}
	return nil
}

// validateFlags enforces agreement between the outcome and the reward flags. A
// verdict claiming a clean attestation while reporting a lost flag is a bug in the
// rule that built it, and it would mislead an operator about whether to act.
func (v Verdict) validateFlags() error {
	if v.Flags == nil {
		if v.Outcome == OutcomeDegraded {
			return fmt.Errorf("%w: outcome degraded requires reward flags", ErrInvalidOutcome)
		}
		return nil
	}
	switch v.Outcome {
	case OutcomeNoDuty:
		return fmt.Errorf("%w: outcome no_duty carries reward flags", ErrInvalidOutcome)
	case OutcomeOK:
		if !v.Flags.AllEarned() {
			return fmt.Errorf("%w: outcome ok lost flags %v", ErrInvalidOutcome, v.Flags.Lost())
		}
	case OutcomeDegraded:
		if v.Flags.AllEarned() {
			return fmt.Errorf("%w: outcome degraded earned every flag", ErrInvalidOutcome)
		}
		if !v.Flags.AnyEarned() {
			return fmt.Errorf("%w: outcome degraded earned no flag, which is missed", ErrInvalidOutcome)
		}
	case OutcomeMissed:
		if v.Flags.AnyEarned() {
			return fmt.Errorf("%w: outcome missed earned flags", ErrInvalidOutcome)
		}
	}
	return nil
}

// ReportedCause returns the most specific cause the verdict reached: the sub-cause
// where one matched, otherwise the cause. This is what a report headline shows.
func (v Verdict) ReportedCause() CauseID {
	if v.SubCause != "" {
		return v.SubCause
	}
	return v.Cause
}

// IsUnknown reports whether the verdict declined to attribute. Tracking the rate of
// these is a project health metric, not an embarrassment (docs/causes.md §7).
func (v Verdict) IsUnknown() bool {
	return v.Cause == CauseInsufficientData || v.Cause == CauseNoRuleMatched
}
