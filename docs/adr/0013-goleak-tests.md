# ADR-0013 · Goroutine leak verification

- **Status:** accepted
- **Date:** 2026-08-23
- **Deciders:** maintainers
- **Supersedes:** —

## Context

BUILD_PROMPT §§5 and 14 require `go.uber.org/goleak` for daemon lifecycle tests.
Race detection does not prove that reconnect, sampling, duty-tracking, and shutdown
goroutines terminate.

## Decision

Use **`go.uber.org/goleak v1.3.0`** as a test-only dependency. The `internal/app`
test process verifies that no unexpected goroutine remains after its full suite,
covering the package that owns daemon wiring and shutdown.

## Consequences

The module gains one direct test dependency and no production binary code. Failures
include leaked stack traces, making lifecycle regressions actionable.

**Removal path.** Delete the `internal/app` `TestMain` hook and the module requirement.
The production API and binary are unaffected.

## Alternatives considered

**Compare `runtime.NumGoroutine`.** Rejected because counts cannot identify leaked
stacks and are unstable under the test runner.

**Timeout-only shutdown tests.** Retained as behavioral tests, but they can miss a
detached goroutine after the observed call returns.
