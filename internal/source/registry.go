// Package source is the composition point for whymiss's inbound adapters
// (beaconapi, promscrape, hostmetrics). registry.go is the one file in this
// tree, besides the adapter packages themselves, allowed to know a
// consensus client's name (I-11) — everything past internal/source deals
// only in domain.Observation and domain.MetricSample, which carry no
// client identity.
package source

import "strings"

// ConsensusClient is the consensus client a beacon node identifies itself
// as. BUILD_PROMPT.md §3 locks the initial client scope to Lighthouse and
// Prysm.
type ConsensusClient string

const (
	// ConsensusLighthouse is Sigma Prime's Lighthouse client.
	ConsensusLighthouse ConsensusClient = "lighthouse"

	// ConsensusPrysm is Offchain Labs' Prysm client.
	ConsensusPrysm ConsensusClient = "prysm"

	// ConsensusUnknown is returned for a version string this build does not
	// recognise — a client outside BUILD_PROMPT.md §3's initial scope, or a
	// node that reported something unparseable. A caller should degrade
	// (fall back to whatever client-agnostic behaviour it has) rather than
	// guess (I-8).
	ConsensusUnknown ConsensusClient = "unknown"
)

// DetectConsensusClient maps a node's self-reported version string (as
// returned by beaconapi.Client.FetchNodeVersion, GET /eth/v1/node/version)
// to a ConsensusClient. Client version strings follow the
// "<ClientName>/v<Version>-<hash>/<platform>" convention — verified against
// a real Lighthouse node in this project's devnet, whose HTTP "Server"
// header (a separate, non-spec signal some clients also set, carrying the
// same value) read "Lighthouse/v8.2.2-e423a66/x86_64-linux".
//
// Prysm detection follows the same publicly documented "Prysm/v..."
// convention but has not yet been verified against a real Prysm node in
// this project's devnet — no /eth/v1/node/version response or Server
// header from Prysm has been captured. Treat this arm as unverified until
// it has (BUILD_PROMPT.md §8's discipline applies to what a test asserts,
// not just to test fixtures: don't claim confidence this package doesn't
// have yet).
func DetectConsensusClient(versionString string) ConsensusClient {
	switch {
	case strings.HasPrefix(versionString, "Lighthouse"):
		return ConsensusLighthouse
	case strings.HasPrefix(versionString, "Prysm"):
		return ConsensusPrysm
	default:
		return ConsensusUnknown
	}
}
