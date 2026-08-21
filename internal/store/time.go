package store

// timeLayout is a fixed-width RFC 3339 layout with nanosecond precision —
// deliberately not time.RFC3339Nano, whose "9" pattern trims trailing
// zeros and so produces strings of varying length (e.g. "...:00Z" for an
// exact second, "...:00.6Z" for 600ms into it). SQLite's ORDER BY on a TEXT
// column is a byte-wise string comparison, and a shorter string can sort
// before a longer one that is chronologically earlier — verified by a
// failing test before this fix: a 0ms observation sorted after a 600ms one
// because "." (0x2E) sorts before "Z" (0x5A). A fixed nine-digit fractional
// part (Go's "0" pattern pads instead of trimming) makes every timestamp
// this package stores the same length, so string order and time order
// agree.
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"
