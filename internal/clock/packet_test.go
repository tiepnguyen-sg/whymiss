package clock

import (
	"testing"
	"time"
)

func TestNTPTimeRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		at   time.Time
	}{
		{"epoch", time.Unix(0, 0).UTC()},
		{"now-ish", time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)},
		{"with sub-second precision", time.Date(2026, 8, 20, 12, 0, 0, 123_456_000, time.UTC)},
		{"within the current NTP era", time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := fromNTP(toNTP(tc.at))
			// The NTP fraction has ~232 picosecond resolution; round-tripping
			// through it can lose a few hundred picoseconds, well under a
			// microsecond. Anything coarser than that would corrupt a clock-offset
			// measurement, so the tolerance is deliberately tight.
			diff := got.Sub(tc.at)
			if diff < 0 {
				diff = -diff
			}
			if diff > time.Microsecond {
				t.Errorf("fromNTP(toNTP(%v)) = %v, diff %s exceeds 1µs", tc.at, got, diff)
			}
			if got.Location() != time.UTC {
				t.Errorf("fromNTP() location = %v, want UTC", got.Location())
			}
		})
	}
}

func TestPacketFields(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	req := newRequest(t1)

	if got := req.mode(); got != modeClient {
		t.Errorf("newRequest().mode() = %d, want %d", got, modeClient)
	}
	if got := req.transmitTimestamp(); got != toNTP(t1) {
		t.Errorf("newRequest().transmitTimestamp() = %d, want %d", got, toNTP(t1))
	}

	var resp packet
	resp[0] = (4 << 3) | modeServer
	resp[1] = 2 // stratum
	if got := resp.mode(); got != modeServer {
		t.Errorf("mode() = %d, want %d", got, modeServer)
	}
	if got := resp.stratum(); got != 2 {
		t.Errorf("stratum() = %d, want 2", got)
	}
	if got := resp.leapIndicator(); got != 0 {
		t.Errorf("leapIndicator() = %d, want 0", got)
	}
}
