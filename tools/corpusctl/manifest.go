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
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Expect      struct {
		Cause      string `yaml:"cause"`
		SubCause   string `yaml:"sub_cause,omitempty"`
		Confidence string `yaml:"confidence"`
	} `yaml:"expect"`

	Slot           uint64 `yaml:"slot"`
	ValidatorIndex uint64 `yaml:"validator_index"`

	FaultKind   string        `yaml:"fault_kind"`
	FaultTarget string        `yaml:"fault_target"`
	Duration    time.Duration `yaml:"duration"`

	GeneratedAt time.Time `yaml:"generated_at"`
}
