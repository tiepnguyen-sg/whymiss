package clock

import (
	"encoding/binary"
	"math"
	"time"
)

// ntpEpochOffset is the number of seconds between the NTP epoch (1900-01-01) and
// the Unix epoch (1970-01-01), per RFC 5905 §6.
const ntpEpochOffset = 2208988800

// modeClient and modeServer are the NTP "Mode" field values this package cares
// about (RFC 5905 Figure 9). Symmetric and control modes are not implemented; a
// server that replies in any mode but modeServer is treated as an invalid response.
const (
	modeClient = 3
	modeServer = 4
)

// leapUnsync is the Leap Indicator value a server sets when its own clock is not
// synchronized. RFC 5905 calls a reply with this LI value, or stratum 0, a
// "kiss-of-death": the server is explicitly saying not to trust it.
const leapUnsync = 3

// packet is the 48-byte NTP/SNTP header (RFC 5905 Figure 8). Only the fields this
// package reads or writes are named; the rest are addressed by offset.
type packet [48]byte

// newRequest builds a client-mode query packet. VN=4 (current protocol version),
// Mode=3 (client). The transmit timestamp doubles as T1: an RFC 5905-compliant
// server echoes it back as the reply's Origin Timestamp, which query() uses to
// reject a stale or off-path response.
func newRequest(t1 time.Time) packet {
	var p packet
	p[0] = (4 << 3) | modeClient
	binary.BigEndian.PutUint64(p[40:48], toNTP(t1))
	return p
}

func (p packet) leapIndicator() byte      { return p[0] >> 6 }
func (p packet) version() byte            { return (p[0] >> 3) & 0x07 }
func (p packet) mode() byte               { return p[0] & 0x07 }
func (p packet) stratum() byte            { return p[1] }
func (p packet) originTimestamp() uint64  { return binary.BigEndian.Uint64(p[24:32]) }
func (p packet) receiveTimestamp() uint64 { return binary.BigEndian.Uint64(p[32:40]) }
func (p packet) transmitTimestamp() uint64 {
	return binary.BigEndian.Uint64(p[40:48])
}

// toNTP converts a wall-clock instant to the 64-bit NTP timestamp format: the
// upper 32 bits are seconds since the NTP epoch, the lower 32 bits are a binary
// fraction of a second.
//
// The 32-bit seconds field wraps in 2036 (NTP "era" rollover, RFC 5905 §7.2).
// Decoding uses fromNTPNear and the local exchange time to disambiguate the era.
func toNTP(t time.Time) uint64 {
	// t.Unix()+ntpEpochOffset is negative only for a time before 1900, which no
	// caller in this package ever passes — every call site here uses time.Now().
	sec := uint64(t.Unix() + ntpEpochOffset) //nolint:gosec // G115: see above, unreachable with this package's inputs
	// t.Nanosecond() is always in [0, 1e9), so the shift cannot lose the sign bit
	// gosec is warning about — there is no sign bit to lose.
	frac := uint64(t.Nanosecond()) << 32 / 1e9 //nolint:gosec // G115: Nanosecond() is bounded to [0, 1e9)
	return sec<<32 | frac
}

// fromNTP decodes era zero and exists for packet-level tests. Network responses
// use fromNTPNear so the 2036 seconds-field rollover is handled correctly.
func fromNTP(v uint64) time.Time {
	sec := int64(v>>32) - ntpEpochOffset
	return ntpTime(sec, v)
}

func fromNTPNear(v uint64, pivot time.Time) time.Time {
	const ntpEraSeconds = int64(1) << 32
	base := int64(v>>32) - ntpEpochOffset
	era := int64(math.Round(float64(pivot.Unix()-base) / float64(ntpEraSeconds)))
	return ntpTime(base+era*ntpEraSeconds, v)
}

func ntpTime(sec int64, v uint64) time.Time {
	frac := v & 0xffffffff
	nsec := frac * 1e9 >> 32
	return time.Unix(sec, int64(nsec)).UTC()
}
