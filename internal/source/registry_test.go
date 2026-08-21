package source

import "testing"

func TestDetectConsensusClient(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    ConsensusClient
	}{
		// Real value captured off this project's devnet (see registry.go's
		// doc comment on DetectConsensusClient).
		{"real captured lighthouse version", "Lighthouse/v8.2.2-e423a66/x86_64-linux", ConsensusLighthouse},
		{"empty string", "", ConsensusUnknown},
		{"unrecognised client", "Nimbus/v24.1.0", ConsensusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectConsensusClient(tt.version); got != tt.want {
				t.Errorf("DetectConsensusClient(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}
