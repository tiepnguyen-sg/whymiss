package domain

import "errors"

// Sentinel errors returned by the constructors in this package. Callers branch on
// these with errors.Is; the wrapped message carries the offending value.
var (
	// ErrInvalidKind reports an observation kind outside the closed vocabulary in
	// docs/causes.md §8.
	ErrInvalidKind = errors.New("observation kind not in taxonomy vocabulary")

	// ErrInvalidAttr reports an attribute key that is undocumented, or documented
	// but not permitted for the observation kind carrying it (docs/causes.md §8.1).
	ErrInvalidAttr = errors.New("attribute key not permitted for this kind")

	// ErrMissingTimestamp reports a zero timestamp. Every fact whymiss records is
	// timestamped; an untimestamped one cannot be placed on a timeline.
	ErrMissingTimestamp = errors.New("timestamp is zero")

	// ErrNotUTC reports a timestamp in a location other than UTC (I-9).
	ErrNotUTC = errors.New("timestamp is not utc")

	// ErrMissingSource reports an unattributed value. Evidence without a source
	// cannot be audited, which defeats the purpose of emitting it.
	ErrMissingSource = errors.New("source is empty")

	// ErrUnsorted reports observations that are not in ascending timestamp order.
	// The engine relies on the ordering being total and deterministic (I-6).
	ErrUnsorted = errors.New("observations are not sorted by timestamp")

	// ErrSlotMismatch reports a value whose slot disagrees with the timeline it was
	// offered to.
	ErrSlotMismatch = errors.New("slot does not match the timeline")

	// ErrNoEvidence reports a verdict with an empty evidence slice (I-7).
	ErrNoEvidence = errors.New("verdict has no evidence")

	// ErrInvalidCause reports a cause ID outside the taxonomy, or a sub-cause that
	// is not a descendant of its parent cause (ADR-0005).
	ErrInvalidCause = errors.New("cause id not in taxonomy")

	// ErrInvalidOutcome reports an outcome outside the closed set, or one that
	// contradicts the reward flags accompanying it (docs/causes.md §2).
	ErrInvalidOutcome = errors.New("outcome invalid for this verdict")

	// ErrInvalidConfidence reports a confidence value outside high, medium, low.
	ErrInvalidConfidence = errors.New("confidence not in taxonomy")

	// ErrMissingVersion reports a verdict that does not record the engine or
	// taxonomy version it was produced under (I-10).
	ErrMissingVersion = errors.New("version is empty")

	// ErrEmptyStatement reports evidence with no statement. Evidence a human cannot
	// read is not evidence.
	ErrEmptyStatement = errors.New("evidence statement is empty")
)
