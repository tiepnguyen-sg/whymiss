// Package clock measures how far the local wall clock is offset from a configured
// NTP server, and degrades honestly when that measurement cannot be trusted.
//
// # Why this package exists
//
// I-9 forbids a timing verdict on an untrusted clock. Every timestamp whymiss
// records travels with the offset measured at sample time (domain.Observation's
// ClockOffset field), and the rule that decides whether a timing attribution may
// proceed (docs/causes.md rule R-011) reads that offset off the timeline. This
// package is where the number comes from.
//
// # Egress
//
// Querying an NTP server is an outbound network call, so I-4 governs it: no server
// is built in, and [Config.Validate] rejects an empty server list. A caller (the
// config package, in Phase 2) supplies servers the operator explicitly configured;
// this package takes no unconfigured action.
//
// An alternative was considered: reading the kernel's own NTP discipline state via
// the adjtimex(2) syscall, which would need no network call at all because the
// host's own NTP daemon (chrony, systemd-timesyncd) already disciplines the clock —
// see docs/causes.md's remediation for local.host.clock_drift, which assumes such a
// daemon is running. That path was set aside for Phase 1: it is Linux-only, needs
// either unsafe per-architecture struct layout or a new dependency
// (golang.org/x/sys) to do safely, and is harder to unit test without a real Linux
// kernel. SNTP over UDP is pure standard library, testable against a loopback
// server, and portable to the darwin development machine as well as the
// linux/amd64 and linux/arm64 release targets. Revisit as ADR-0006 if operators
// report the network round trip is itself unreliable on the boxes this runs on.
//
// # Degradation, not fabrication
//
// [Sampler.Sample] returns a typed error rather than a fabricated offset when every
// configured server fails within the bounded retry budget (I-5: retries are
// exponential with jitter and bounded — never a retry storm). [Tracker] additionally
// remembers the last successful reading, because docs/causes.md requires
// local.host.clock_drift evidence to name "time of last successful sync": a caller
// facing a failed measurement can still report when the clock was last known good,
// which is materially more honest than silence.
//
// This package does not decide whether an offset is trustworthy — it reports what
// it measured. The threshold comparison belongs to the rule that reads it
// (docs/causes.md §4, thresholds.clock_offset_max), keeping the decision in one
// place rather than duplicated here.
package clock
