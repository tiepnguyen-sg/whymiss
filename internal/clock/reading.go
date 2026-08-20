package clock

import "time"

// Reading is one successful offset measurement.
type Reading struct {
	// Server is the address the reading came from, exactly as configured. Kept on
	// the reading rather than discarded because it belongs in the evidence a
	// timing verdict cites.
	Server string

	// At is the local receive time of the server's reply (T4 in the SNTP
	// exchange), in UTC. It is when the measurement was taken, not when the
	// caller later consumes it.
	At time.Time

	// Offset is the server's clock minus the local clock: a positive value means
	// the local clock is behind. Adding Offset to a local timestamp corrects it
	// toward the server's time.
	Offset time.Duration

	// RoundTrip is the measured network delay of the exchange. A large round trip
	// widens the true error bound on Offset even when the computed value looks
	// precise; callers wanting a stricter trust bound may factor this in.
	RoundTrip time.Duration
}
