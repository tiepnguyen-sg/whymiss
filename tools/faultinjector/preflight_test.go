package main

import "testing"

// TestRequiredPeers pins the whole-mesh rule. Its predecessor asserted a fixed
// `minPreflightPeers == 1` and justified it as "the corpus devnet has two
// consensus nodes" — true when written, stale the moment a third participant was
// added for network.late_block, and guarding the stale premise instead of
// catching it. Deriving the requirement from the enclave's own node count means
// the topology can change without the check quietly weakening.
func TestRequiredPeers(t *testing.T) {
	t.Parallel()
	for nodes, want := range map[int]int{2: 1, 3: 2, 5: 4} {
		if got := requiredPeers(nodes); got != want {
			t.Errorf("requiredPeers(%d) = %d, want %d", nodes, got, want)
		}
	}
}

func TestConsensusServiceName(t *testing.T) {
	t.Parallel()

	// Real `kurtosis enclave inspect` output, including the rows preflight must
	// ignore: execution clients, validator clients, and the added services.
	lines := []string{
		"eb02eaaf4a7b   cl-1-lighthouse-geth                             http: 4000/tcp -> http://127.0.0.1:32962      RUNNING",
		"39781110219d   cl-2-prysm-geth                                  http: 3500/tcp -> http://127.0.0.1:32948      RUNNING",
		"4007f2cde94e   cl-3-prysm-geth                                  http: 3500/tcp -> http://127.0.0.1:32953      RUNNING",
		"1bfa1638841d   el-1-geth-lighthouse                             rpc: 8545/tcp -> http://127.0.0.1:32940       RUNNING",
		"aa11bb22cc33   vc-1-geth-lighthouse                             metrics: 8080/tcp -> http://127.0.0.1:32941   RUNNING",
		"dd44ee55ff66   spamoor                                          http: 8080/tcp -> http://127.0.0.1:32999      RUNNING",
	}
	var got []string
	for _, line := range lines {
		if name, ok := consensusServiceName(line); ok {
			got = append(got, name)
		}
	}
	want := []string{"cl-1-lighthouse-geth", "cl-2-prysm-geth", "cl-3-prysm-geth"}
	if len(got) != len(want) {
		t.Fatalf("matched %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("matched[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
