package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

// exportObserved writes a corpus record from observations a running collector
// already recorded, for a condition the network produced on its own.
//
// It exists because some causes cannot be injected. network.payload_late needs
// an ePBS payload revealed late, which no tooling here can create and which must
// never be inflicted on a shared public testnet (ADR-0027). The condition does
// occur on public Glamsterdam networks, so the only honest way to measure the
// cause is to record it where it happens.
//
// The record it writes is marked origin: observed, and `make eval` reports those
// separately from injected ones. That separation is the point: an injected
// record's label comes from what the harness did and is independent of anything
// whymiss saw, while an observed record's label and the rule under test read the
// same on-chain fact. Both are useful; conflating them would quietly change what
// the headline accuracy figure means.
func exportObserved(args []string) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	dbPath := flags.String("db", "", "collector database to read observations from")
	slot := flags.Uint64("slot", 0, "slot to export")
	out := flags.String("out", "", "directory to write the record into (its base name becomes the record id)")
	network := flags.String("network", "", "network the observations came from, recorded in the description")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *out == "" || *slot == 0 {
		return fmt.Errorf("usage: corpusctl export --db <path> --slot <n> --out <dir> [--network <name>]")
	}

	ctx := context.Background()
	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close() //nolint:errcheck // read-only export; a close error changes nothing already written

	observations, err := st.ObservationsForSlot(ctx, domain.Slot(*slot))
	if err != nil {
		return fmt.Errorf("read observations for slot %d: %w", *slot, err)
	}
	if len(observations) == 0 {
		return fmt.Errorf("slot %d has no observations in %s", *slot, *dbPath)
	}

	if err := os.MkdirAll(*out, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", *out, err)
	}
	obsPath := filepath.Join(*out, "observations.jsonl")
	encoded, err := encodeObservations(observations)
	if err != nil {
		return err
	}
	if err := os.WriteFile(obsPath, encoded, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", obsPath, err)
	}

	sum := sha256.Sum256(encoded)
	fmt.Printf("corpusctl: wrote %d observations to %s\n", len(observations), obsPath)
	fmt.Printf("corpusctl: observations_sha256: %s\n", hex.EncodeToString(sum[:]))
	fmt.Printf("corpusctl: kinds: %s\n", kindsOf(observations))
	fmt.Printf("corpusctl: network: %s, exported %s\n", *network, time.Now().UTC().Format(time.RFC3339))
	fmt.Println("corpusctl: write manifest.yaml beside it with origin: observed, the schedule the run used, and an expect: block")
	return nil
}

// encodeObservations writes one JSON object per line, in the order the store
// returned them, which is the order timeline.Replay expects.
func encodeObservations(observations []domain.Observation) ([]byte, error) {
	var buf []byte
	for i, obs := range observations {
		line, err := json.Marshal(obs)
		if err != nil {
			return nil, fmt.Errorf("encode observation %d: %w", i, err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return buf, nil
}

func kindsOf(observations []domain.Observation) string {
	seen := make([]string, 0, len(observations))
	for _, obs := range observations {
		seen = append(seen, string(obs.Kind))
	}
	return fmt.Sprint(seen)
}
