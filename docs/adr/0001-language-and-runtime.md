# ADR-0001 · Language and runtime

- **Status:** accepted
- **Date:** 2026-08-20
- **Deciders:** maintainers
- **Supersedes:** —

## Context

whymiss is installed by solo stakers onto the machine running their validator. That
machine is frequently a Raspberry Pi 5 or a small VPS already at 70% memory
utilisation, and its operator is, correctly, hostile to new software near their keys.

The runtime therefore has to satisfy constraints that are unusual for a diagnostic
tool:

- Installation must not pull in a language runtime, a package manager, or a system
  library. Every such dependency is a reason for the operator to decline.
- The artifact must be auditable by the operator. "Read the source, build it
  yourself, diff the checksum" has to be realistic in an afternoon.
- It must cross-compile to `linux/arm64` from CI without emulation, because a
  meaningful share of solo stakers run ARM single-board computers.
- The Ethereum consensus-layer ecosystem it integrates with — Lighthouse, Prysm,
  Teku, Nimbus, Erigon, Geth — is largely Go, Rust, Nim, and Java. Type definitions,
  metric semantics, and SSZ/API conventions are easiest to mirror correctly in a
  language the client teams themselves publish libraries for.

## Decision

**Go 1.23 or later, compiled with `CGO_ENABLED=0`, shipped as a single static
binary.**

The minimum version is declared once, in `go.mod`. CI derives its toolchain from
that line (`setup-go` with `go-version-file: go.mod`) so the declared floor and the
tested floor cannot drift apart.

`CGO_ENABLED=0` is set in the `Makefile` as an exported variable rather than passed
per-target, so no build path can accidentally omit it. This is the mechanical
enforcement of I-13.

Consequences of that flag are accepted deliberately: it forecloses any C library,
which is why storage is constrained to a pure-Go driver (ADR-0002) and why Parquet
access, when it arrives in Phase 5, must use a pure-Go reader.

## Consequences

**Good**

- One file to install, one file to delete. Uninstall is `rm`, which is a genuine
  selling point to a suspicious operator.
- `GOOS`/`GOARCH` cross-compilation covers amd64 and arm64 from a single CI runner
  in seconds, no QEMU.
- The standard library covers HTTP, TLS, JSON, SQL plumbing, structured logging, and
  testing, which makes the dependency austerity target in ADR-0004 achievable rather
  than aspirational.
- Goroutine-per-source concurrency matches the problem shape: several independent
  pollers feeding one timeline assembler.
- Operators already run Go binaries — geth, Prysm, Lighthouse's tooling. The
  deployment model is familiar and needs no explanation in the README.

**Bad**

- No CGO means no `mattn/go-sqlite3`, the most battle-tested SQLite binding. See
  ADR-0002 for how that constraint is resolved.
- Go's garbage collector makes hard real-time memory ceilings awkward. I-12 is
  therefore enforced by explicit bounds — capped buffers, retention by byte count,
  a soak test asserting RSS — rather than by trusting the runtime.
- Generics are available but the codebase is expected to prefer concrete types, per
  the standard-library style §5 mandates. Contributors arriving from other
  ecosystems may find this austere.

## Alternatives considered

**Rust.** Better memory-footprint story and a strong presence in the Ethereum
tooling ecosystem (Reth, Lighthouse). Rejected on maintainability grounds: the async
ecosystem choice would itself need an ADR, compile times slow the corpus-driven test
loop that Phase 1 is built around, and the contributor pool for "add an adapter for
my client" is smaller. The footprint advantage does not decide the question because
the binary is I/O-bound and idle most of the time.

**Python.** Fastest to prototype the RCA rules, and the Ethereum research ecosystem
uses it heavily. Rejected outright: it violates I-13 and asks the operator to install
an interpreter and a dependency tree next to their validator keys. That is precisely
the install the target user refuses.

**Zig or C.** Smallest artifact. Rejected: no realistic contributor pool for a tool
whose value depends on external contributors adding client adapters, and manual
memory management is an unnecessary source of security findings in a process running
beside staking infrastructure.
