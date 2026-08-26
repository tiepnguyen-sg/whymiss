package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/exporter"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func headObs(t *testing.T, slot domain.Slot) domain.Observation {
	t.Helper()
	obs, err := domain.NewObservation(domain.Observation{
		Slot: slot, Kind: domain.ObsHeadUpdated, At: time.Now().UTC(), Source: domain.SourceBeaconAPI,
		Attrs: map[domain.AttrKey]string{domain.AttrBlockRoot: "0x" + fmt.Sprintf("%064x", 1)},
	})
	if err != nil {
		t.Fatalf("build head_updated: %v", err)
	}
	return obs
}

func TestHeadFanout_SendReachesEveryWiredCollector(t *testing.T) {
	t.Parallel()
	heads := headFanout{
		timing:   make(chan domain.Observation, 1),
		baseline: make(chan domain.Observation, 1),
	}
	head := headObs(t, 100)
	heads.send(head, discardLogger())

	for name, jobs := range map[string]chan domain.Observation{"timing": heads.timing, "baseline": heads.baseline} {
		select {
		case got := <-jobs:
			if got.Slot != head.Slot {
				t.Errorf("%s received slot %d, want %d", name, got.Slot, head.Slot)
			}
		default:
			t.Errorf("%s collector received nothing", name)
		}
	}
}

func TestHeadFanout_SendIsSafeWhenPartiallyWired(t *testing.T) {
	t.Parallel()
	// An operator running --cl-metrics-api without --baseline-beacon-api (or
	// neither) is the common case, so a nil channel must be skipped rather
	// than block or panic.
	heads := headFanout{baseline: make(chan domain.Observation, 1)}
	heads.send(headObs(t, 100), discardLogger())
	if len(heads.baseline) != 1 {
		t.Errorf("baseline queue holds %d, want 1", len(heads.baseline))
	}

	var unwired headFanout
	unwired.send(headObs(t, 101), discardLogger())
	var absent *headFanout
	absent.send(headObs(t, 102), discardLogger())
}

func TestHeadFanout_SendDropsRatherThanBlocks(t *testing.T) {
	t.Parallel()
	// I-12 bounds the queue at one pending head, and I-5 forbids letting a
	// slow metrics endpoint delay the loop that feeds it. A second head
	// arriving while the first is unread must be dropped, not queued and not
	// blocked on.
	heads := headFanout{timing: make(chan domain.Observation, 1)}
	heads.send(headObs(t, 100), discardLogger())

	done := make(chan struct{})
	go func() {
		heads.send(headObs(t, 101), discardLogger())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("send blocked on a full collector queue")
	}

	got := <-heads.timing
	if got.Slot != 100 {
		t.Errorf("queued slot %d, want the first head 100 kept", got.Slot)
	}
	if len(heads.timing) != 0 {
		t.Errorf("queue holds %d after draining, want the second head dropped", len(heads.timing))
	}
}

func TestHeadFanout_SendIgnoresOtherObservationKinds(t *testing.T) {
	t.Parallel()
	// Callers hand this whatever the stream produced. Only head_updated
	// carries the per-slot timing anchor both collectors scrape against;
	// forwarding anything else would make them sample for the wrong instant.
	obs, err := domain.NewObservation(domain.Observation{
		Slot: 100, Kind: domain.ObsSlotStart, At: time.Now().UTC(), Source: domain.SourceDerived,
	})
	if err != nil {
		t.Fatalf("build slot_start: %v", err)
	}
	heads := headFanout{timing: make(chan domain.Observation, 1), baseline: make(chan domain.Observation, 1)}
	heads.send(obs, discardLogger())
	if len(heads.timing)+len(heads.baseline) != 0 {
		t.Error("a non-head observation was forwarded to a head-driven collector")
	}
}

// TestTrackDuty_RESTHeadReachesEveryCollector is the regression guard for the
// defect headFanout exists to prevent: the REST head poll fed block timing but
// not the network baseline, so on a node whose Beacon API does not serve
// /eth/v1/events the baseline was never collected, tl.Network stayed nil, and
// R-110 and R-200 could never attribute a propagation delay. The fake server
// here deliberately offers no event stream at all — that is the whole point of
// the case.
func TestTrackDuty_RESTHeadReachesEveryCollector(t *testing.T) {
	const (
		dutySlot     = domain.Slot(64)
		slotDuration = time.Second
	)
	root := "0x" + fmt.Sprintf("%064x", 0xabc)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/eth/v1/beacon/headers/head" {
			_, _ = fmt.Fprintf(w, `{"execution_optimistic":false,"data":{"root":%q,"canonical":true,"header":{"message":{"slot":"%d","proposer_index":"7"}}}}`, root, dutySlot)
			return
		}
		// Every other endpoint is absent, exactly as on a node that serves
		// only what this test needs. trackDuty logs and continues.
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "whymiss.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("store.Close: %v", err)
		}
	}()

	genesis := beaconapi.GenesisInfo{
		GenesisTime:    time.Now().UTC().Add(-time.Duration(dutySlot) * slotDuration),
		SecondsPerSlot: slotDuration,
	}
	heads := headFanout{
		timing:   make(chan domain.Observation, 1),
		baseline: make(chan domain.Observation, 1),
	}

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		duty := beaconapi.AttesterDuty{
			Duty: domain.Duty{Kind: domain.DutyAttester, Slot: dutySlot, ValidatorIndex: 24},
		}
		trackDuty(ctx, st, beaconapi.NewClient(srv.URL, 0), duty,
			genesis, dbPath, domain.MainnetPreEPBS(), rca.DefaultConfig(),
			exporter.New(), &heads, nil, time.Minute, discardLogger())
	}()

	for name, jobs := range map[string]chan domain.Observation{"timing": heads.timing, "baseline": heads.baseline} {
		select {
		case got := <-jobs:
			if got.Slot != dutySlot {
				t.Errorf("%s received slot %d, want %d", name, got.Slot, dutySlot)
			}
			if got.Kind != domain.ObsHeadUpdated {
				t.Errorf("%s received kind %q, want %q", name, got.Kind, domain.ObsHeadUpdated)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("%s collector never received the REST-polled head; a node without an event stream would collect nothing for it", name)
		}
	}

	cancel()
	select {
	case <-returned:
	case <-time.After(15 * time.Second):
		t.Fatal("trackDuty did not return after cancellation")
	}
}
