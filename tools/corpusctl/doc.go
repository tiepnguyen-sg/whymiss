// Command corpusctl validates the format of test/corpus scenarios: that
// manifest.yaml is well-formed and labels a real taxonomy cause, and that
// observations.jsonl decodes as a sorted sequence of valid domain.Observation
// values for the manifest's slot.
//
// It does not judge whether a scenario's label is *correct* — that is what
// internal/rca's golden tests and `make eval` are for, once the engine exists
// (Phase 3). corpusctl only enforces that the scenario is well-formed enough to
// be loaded: task 1.6's schema check, not an accuracy check.
package main
