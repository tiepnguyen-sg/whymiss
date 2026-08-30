package main

import "time"

// manifest mirrors tools/faultinjector's Manifest type.
//
// Duplicated rather than imported: both are independent `package main` binaries
// (Go does not allow importing one main package from another), and STRUCTURE.md's
// canonical tree has no shared library package for tools/ to factor this into
// without inventing a directory it does not list. The two are kept in sync by
// hand — a small, stable struct, and a cost worth paying to avoid a directory
// neither BUILD_PROMPT nor STRUCTURE.md calls for.
type manifest struct {
	CorpusFormatVersion    int    `yaml:"corpus_format_version"`
	GeneratorEngineVersion string `yaml:"generator_engine_version"`
	ID                     string `yaml:"id"`
	RecipeID               string `yaml:"recipe_id,omitempty"`

	// Origin says how the record's condition came about, and it changes what the
	// record is evidence of.
	//
	// "injected" is the default and the stronger form: the label comes from what
	// the harness did to the system — cap the execution client's CPU, expect
	// local.el_slow — which is independent of anything whymiss observed. That is
	// what makes it a test of attribution.
	//
	// "observed" records a condition the network produced on its own. It is
	// weaker evidence of attribution, because the label and the rule read the
	// same on-chain fact, and it is admitted anyway because some causes cannot be
	// injected at all: network.payload_late needs a late ePBS payload, which no
	// tooling here can create and which must never be inflicted on a shared
	// public testnet (ADR-0027). What it does test is real: the collection path
	// and the rule's gates against data nobody staged.
	//
	// Empty means "injected", so every record written before this field existed
	// keeps its meaning.
	Origin      string `yaml:"origin,omitempty"`
	Description string `yaml:"description"`
	Expect      struct {
		Cause      string `yaml:"cause"`
		SubCause   string `yaml:"sub_cause,omitempty"`
		Confidence string `yaml:"confidence"`
	} `yaml:"expect"`

	// Schedule is the slot timing the record was collected under. Omitted means
	// domain.MainnetPreEPBS(), which is what every pre-Glamsterdam record ran on.
	// A post-ePBS record must state it, because the deadlines a verdict was
	// measured against are part of what the record is evidence of (ADR-0026).
	Schedule *struct {
		SecondsPerSlot        string `yaml:"seconds_per_slot"`
		AttestationDeadline   string `yaml:"attestation_deadline"`
		AggregationDeadline   string `yaml:"aggregation_deadline"`
		PayloadRevealDeadline string `yaml:"payload_reveal_deadline,omitempty"`
		PTCDeadline           string `yaml:"ptc_deadline,omitempty"`
	} `yaml:"schedule,omitempty"`

	Slot           uint64 `yaml:"slot"`
	ValidatorIndex uint64 `yaml:"validator_index"`

	FaultKind   string        `yaml:"fault_kind"`
	FaultTarget string        `yaml:"fault_target"`
	Duration    time.Duration `yaml:"duration"`

	GeneratedAt  time.Time `yaml:"generated_at"`
	ClockSamples []struct {
		Server    string        `yaml:"server"`
		SampleAt  time.Time     `yaml:"sampled_at"`
		Offset    time.Duration `yaml:"offset"`
		RoundTrip time.Duration `yaml:"round_trip"`
	} `yaml:"clock_samples"`
	ObservationsSHA256 string `yaml:"observations_sha256"`
	SamplesSHA256      string `yaml:"samples_sha256,omitempty"`
}
