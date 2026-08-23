// Package hostmetrics is the inbound adapter for the machine whymiss runs
// on: Linux PSI I/O pressure, CPU steal, and memory pressure (BUILD_PROMPT.md §4,
// task 2.3). Clock drift is internal/clock's job (task 1.4), not this
// package's — a caller assembling a domain.Timeline combines both.
//
// Every reading here is Linux-specific (/proc/pressure/*, /proc/stat) and
// degrades gracefully rather than panicking when the file is absent (I-3) —
// true on any non-Linux host, and true on a Linux host whose kernel
// predates PSI (pre-4.20) for the pressure files specifically. A caller
// gets a clear error either way, never a fabricated zero.
package hostmetrics
