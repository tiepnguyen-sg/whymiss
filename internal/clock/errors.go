package clock

type sentinelError string

func (e sentinelError) Error() string { return string(e) }

const (
	// ErrNoServers reports a Config with an empty server list. There is
	// deliberately no built-in default (I-4): a caller must configure at least one.
	ErrNoServers sentinelError = "clock: no NTP servers configured"

	// ErrInvalidConfig reports a Config field outside its valid range.
	ErrInvalidConfig sentinelError = "clock: invalid configuration"

	// ErrInvalidResponse reports a server reply that is too short, malformed, or a
	// kiss-of-death (stratum 0) response. Treated the same as a timeout: the server
	// cannot be trusted for this attempt.
	ErrInvalidResponse sentinelError = "clock: invalid response from server"

	// ErrAllAttemptsFailed reports that the bounded retry budget was exhausted
	// without a usable reading. The wrapped error carries the most recent attempt's
	// failure.
	ErrAllAttemptsFailed sentinelError = "clock: all measurement attempts failed"
)
