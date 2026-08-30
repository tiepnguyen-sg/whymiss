package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
)

// specServer answers /eth/v1/config/spec with body, and 404s everything else.
func specServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eth/v1/config/spec" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const gloasSpecBody = `{"data":{"SECONDS_PER_SLOT":"12","ATTESTATION_DUE_BPS":"3333",` +
	`"ATTESTATION_DUE_BPS_GLOAS":"2500","AGGREGATE_DUE_BPS":"6667","PAYLOAD_DUE_BPS":"5000",` +
	`"PAYLOAD_ATTESTATION_DUE_BPS":"7500","GLOAS_FORK_EPOCH":"2"}}`

func genesisAt(t *testing.T, epochsAgo int) beaconapi.GenesisInfo {
	t.Helper()

	return beaconapi.GenesisInfo{
		GenesisTime:    time.Now().UTC().Add(-time.Duration(epochsAgo) * domain.SlotsPerEpoch * 12 * time.Second),
		SecondsPerSlot: 12 * time.Second,
	}
}

func TestAdoptNodeSchedule(t *testing.T) {
	t.Parallel()

	t.Run("adopts the node's schedule when the operator configured none", func(t *testing.T) {
		t.Parallel()

		srv := specServer(t, http.StatusOK, gloasSpecBody)
		logger, _ := newTestLogger()

		got := adoptNodeSchedule(context.Background(), beaconapi.NewClient(srv.URL, 0),
			genesisAt(t, 10), domain.MainnetPreEPBS(), logger)

		if !got.IsPostEPBS() {
			t.Fatalf("schedule = %+v, want the node's post-ePBS one", got)
		}
		if got.AttestationDeadline != 3*time.Second || got.PayloadRevealDeadline != 6*time.Second {
			t.Errorf("schedule = %+v", got)
		}
	})

	t.Run("an operator's own schedule wins", func(t *testing.T) {
		t.Parallel()

		srv := specServer(t, http.StatusOK, gloasSpecBody)
		logger, _ := newTestLogger()

		configured := domain.SlotSchedule{
			SecondsPerSlot:      12 * time.Second,
			AttestationDeadline: 5 * time.Second,
			AggregationDeadline: 9 * time.Second,
		}
		got := adoptNodeSchedule(context.Background(), beaconapi.NewClient(srv.URL, 0),
			genesisAt(t, 10), configured, logger)

		if got != configured {
			t.Errorf("schedule = %+v, want the configured %+v", got, configured)
		}
	})

	t.Run("a node that cannot answer leaves the schedule alone", func(t *testing.T) {
		t.Parallel()

		for name, srv := range map[string]*httptest.Server{
			"error":       specServer(t, http.StatusInternalServerError, `{}`),
			"no spec":     specServer(t, http.StatusNotFound, ``),
			"empty spec":  specServer(t, http.StatusOK, `{"data":{}}`),
			"missing key": specServer(t, http.StatusOK, `{"data":{"SECONDS_PER_SLOT":"12"}}`),
		} {
			logger, _ := newTestLogger()
			got := adoptNodeSchedule(context.Background(), beaconapi.NewClient(srv.URL, 0),
				genesisAt(t, 10), domain.MainnetPreEPBS(), logger)
			if got != domain.MainnetPreEPBS() {
				t.Errorf("%s: schedule = %+v, want the configured default", name, got)
			}
		}
	})

	t.Run("the chain has not reached a scheduled fork", func(t *testing.T) {
		t.Parallel()

		srv := specServer(t, http.StatusOK, gloasSpecBody)
		logger, _ := newTestLogger()

		// Genesis one epoch ago, GLOAS_FORK_EPOCH 2: the fork is coming, not here.
		got := adoptNodeSchedule(context.Background(), beaconapi.NewClient(srv.URL, 0),
			genesisAt(t, 1), domain.MainnetPreEPBS(), logger)

		if got.IsPostEPBS() {
			t.Errorf("schedule = %+v, want pre-ePBS before the fork epoch", got)
		}
	})
}
