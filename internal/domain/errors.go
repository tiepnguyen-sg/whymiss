package domain

type sentinelError string

func (e sentinelError) Error() string { return string(e) }

// Sentinel errors returned by the constructors in this package. Callers branch on
// these with errors.Is; the wrapped message carries the offending value.
const (
	// ErrInvalidKind reports an observation kind outside the closed vocabulary in
	// docs/causes.md §8.
	ErrInvalidKind sentinelError = "observation kind not in taxonomy vocabulary"

	// ErrInvalidAttr reports an attribute key that is undocumented, or documented
	// but not permitted for the observation kind carrying it (docs/causes.md §8.1).
	ErrInvalidAttr sentinelError = "attribute key not permitted for this kind"

	// ErrMissingTimestamp reports a zero timestamp. Every fact whymiss records is
	// timestamped; an untimestamped one cannot be placed on a timeline.
	ErrMissingTimestamp sentinelError = "timestamp is zero"

	// ErrNotUTC reports a timestamp in a location other than UTC (I-9).
	ErrNotUTC sentinelError = "timestamp is not utc"

	// ErrInvalidClock reports an observation whose clock metadata is
	// contradictory or incomplete. A measured offset without the instant it was
	// measured cannot establish freshness, and an unmeasured offset must not
	// carry a fabricated non-zero value (I-9).
	ErrInvalidClock sentinelError = "clock measurement metadata is invalid"

	// ErrMissingSource reports an unattributed value. Evidence without a source
	// cannot be audited, which defeats the purpose of emitting it.
	ErrMissingSource sentinelError = "source is empty"

	// ErrInvalidSource reports a known source attached to an observation kind it
	// cannot produce. Source attribution is evidence, not free-form metadata.
	ErrInvalidSource sentinelError = "source is not permitted for observation kind"

	// ErrUnsorted reports observations that are not in ascending timestamp order.
	// The engine relies on the ordering being total and deterministic (I-6).
	ErrUnsorted sentinelError = "observations are not sorted by timestamp"

	// ErrSlotMismatch reports a value whose slot disagrees with the timeline it was
	// offered to.
	ErrSlotMismatch sentinelError = "slot does not match the timeline"

	// ErrNoEvidence reports a verdict with an empty evidence slice (I-7).
	ErrNoEvidence sentinelError = "verdict has no evidence"

	// ErrInvalidCause reports a cause ID outside the taxonomy, or a sub-cause that
	// is not a descendant of its parent cause (ADR-0005).
	ErrInvalidCause sentinelError = "cause id not in taxonomy"

	// ErrInvalidOutcome reports an outcome outside the closed set, or one that
	// contradicts the reward flags accompanying it (docs/causes.md §2).
	ErrInvalidOutcome sentinelError = "outcome invalid for this verdict"

	// ErrInvalidConfidence reports a confidence value outside high, medium, low.
	ErrInvalidConfidence sentinelError = "confidence not in taxonomy"

	// ErrMissingVersion reports a verdict that does not record the engine or
	// taxonomy version it was produced under (I-10).
	ErrMissingVersion sentinelError = "version is empty"

	// ErrEmptyStatement reports evidence with no statement. Evidence a human cannot
	// read is not evidence.
	ErrEmptyStatement sentinelError = "evidence statement is empty"
)
