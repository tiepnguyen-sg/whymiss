// Command faultinjector reproducibly injects a declared fault into the running
// Kurtosis devnet (test/e2e/kurtosis), watches the target validator's duty through
// the fault window via the beacon API, and writes the result as a corpus scenario
// under test/corpus/<scenario-id>/ — a manifest.yaml naming the expected cause and
// an observations.jsonl of what was actually observed.
//
// # Why this exists
//
// docs/BUILD_PROMPT.md §8 forbids hand-writing a mock beacon-node response: every
// corpus scenario has to come from a real client actually failing in a real way.
// A human manually pulling a network cable and copying down timestamps does not
// scale past a handful of scenarios and does not reproduce identically next time.
// This tool is that manual procedure, made repeatable: `make corpus.generate
// SCENARIO=el-disk-stall` produces the same shape of evidence today and a year
// from now, against whatever client versions the devnet config pins at the time.
//
// # Scenarios are declarative
//
// A scenario is a YAML file under tools/faultinjector/scenarios/ naming: which
// running service to target, which fault to apply and for how long, and which
// cause the taxonomy predicts it will produce. Adding a scenario is adding a file,
// not writing Go — see [Scenario] and scenarios/README.md.
//
// # Fault mechanisms
//
// One file per mechanism (fault_*.go), each satisfying the [Fault] interface.
// BUILD_PROMPT task 1.5 names five: tc netem (network degradation), cgroup io.max
// (disk throttling), container pause (process freeze), libfaketime (clock skew),
// and peer drop (P2P isolation). Each shells out to `kurtosis service exec`
// against the target container rather than linking a Kurtosis SDK — zero new Go
// dependency (ADR-0004), and identical to how a human operator would reach into
// the same container by hand.
//
// # No dependency on internal/domain's purity boundary
//
// This tool is not internal/ or cmd/, so it is free of I-6/I-11's import
// restrictions — and it uses that freedom: it names clients and calls the beacon
// API directly, because provoking and observing a real client failure is exactly
// its job. What it must still honor is I-7 downstream: every observation it
// records is a fact it actually measured, never a value it invented to make a
// scenario look clean.
package main
