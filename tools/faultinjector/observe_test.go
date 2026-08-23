package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestPollBlockSeenConfirmsSkippedSlot(t *testing.T) {
	fixture, err := os.ReadFile("../../internal/source/beaconapi/testdata/node_syncing.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/eth/v1/node/syncing" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	observer := &Observer{BeaconAPI: server.URL, Client: server.Client()}
	got, err := observer.PollBlockSeen(context.Background(), 788, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatalf("PollBlockSeen: %v", err)
	}
	if !got.Skipped || got.Found || got.At.IsZero() {
		t.Fatalf("status = %+v, want a timestamped skipped slot", got)
	}
}

func TestPollBlockSeenDoesNotCallCurrentHeadSkipped(t *testing.T) {
	fixture, err := os.ReadFile("../../internal/source/beaconapi/testdata/node_syncing.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/eth/v1/node/syncing" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	// A budget this small still exercises the give-up path: the status is
	// sampled once, found inconclusive, and the deadline has already passed.
	observer := &Observer{BeaconAPI: server.URL, Client: server.Client(), NodeRecoveryBudget: time.Nanosecond}
	got, err := observer.PollBlockSeen(context.Background(), 789, time.Now().Add(-time.Second))
	if got.Skipped || got.Found {
		t.Fatalf("status = %+v, want no conclusion at the node's current head", got)
	}
	if err == nil {
		t.Fatal("PollBlockSeen: inconclusive current-head status must block corpus generation")
	}
}

func TestBuildObservationsRecordsConfirmedSkip(t *testing.T) {
	slotStart := time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC)
	reading := clock.Reading{At: slotStart.Add(-time.Minute), Server: "127.0.0.1:123"}
	observations, err := buildObservations(
		Scenario{ValidatorIndex: 7}, 788, slotStart, slotStart.Add(-time.Hour),
		dutyOutcome{BlockSkipped: true, BlockSeenAt: slotStart.Add(36 * time.Second)},
		[]clock.Reading{reading},
	)
	if err != nil {
		t.Fatalf("buildObservations: %v", err)
	}
	for _, observation := range observations {
		if observation.Kind == domain.ObsBlockSkipped {
			return
		}
	}
	t.Fatal("buildObservations omitted block_skipped")
}

func TestBuildObservationsRecordsCompletionAndNetworkBaseline(t *testing.T) {
	t.Parallel()

	slotStart := time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC)
	reading := clock.Reading{
		At: slotStart.Add(-time.Minute), Offset: 25 * time.Millisecond,
		Server: "127.0.0.1:123",
	}
	completedAt := slotStart.Add(10 * time.Minute)
	completionReading := clock.Reading{
		At: completedAt, Offset: 30 * time.Millisecond,
		Server: "127.0.0.1:123",
	}
	baselineAt := slotStart.Add(2 * time.Second)
	observations, err := buildObservations(
		Scenario{ValidatorIndex: 7}, 788, slotStart, slotStart.Add(-time.Hour),
		dutyOutcome{
			CollectionCompletedAt: completedAt,
			Network: &domain.NetworkBaseline{
				Slot: 788, BlockArrivalP50: 850 * time.Millisecond,
				BlockArrivalP90: 1300 * time.Millisecond, SampleCount: 42,
				Source: domain.SourcePromScrape,
			},
			NetworkSampledAt: baselineAt,
		},
		[]clock.Reading{reading, completionReading},
	)
	if err != nil {
		t.Fatalf("buildObservations: %v", err)
	}

	var completion, baseline *domain.Observation
	for i := range observations {
		switch observations[i].Kind {
		case domain.ObsCollectionCompleted:
			completion = &observations[i]
		case domain.ObsNetworkBaselineSampled:
			baseline = &observations[i]
		}
	}
	if completion == nil || !completion.At.Equal(completedAt.Add(completionReading.Offset)) || completion.Source != domain.SourceDerived {
		t.Fatalf("collection completion = %+v, want clock-corrected derived observation at %s", completion, completedAt.Add(completionReading.Offset))
	}
	if got, ok := completion.Attr(domain.AttrValidatorIndex); !ok || got != "7" {
		t.Errorf("collection completion validator_index = %q, %t, want 7, true", got, ok)
	}
	if !completion.ClockSampleAt.Equal(completionReading.At.Add(completionReading.Offset)) {
		t.Errorf("completion clock sample = %s, want completion reading %s", completion.ClockSampleAt, completionReading.At.Add(completionReading.Offset))
	}
	if baseline == nil {
		t.Fatal("buildObservations omitted network_baseline_sampled")
	}
	if !baseline.ClockSampleAt.Equal(reading.At.Add(reading.Offset)) {
		t.Errorf("baseline clock sample = %s, want early reading %s", baseline.ClockSampleAt, reading.At.Add(reading.Offset))
	}
	if !baseline.At.Equal(baselineAt) {
		t.Errorf("baseline time = %s, want unshifted scrape time %s", baseline.At, baselineAt)
	}
	if got, _ := baseline.Attr(domain.AttrSampleCount); got != "42" {
		t.Errorf("sample_count = %q, want 42", got)
	}
	if got, _ := baseline.Attr(domain.AttrBlockArrivalP50MS); got != "850" {
		t.Errorf("block_arrival_p50_ms = %q, want 850", got)
	}
	for i := 1; i < len(observations); i++ {
		if observations[i].At.Before(observations[i-1].At) {
			t.Fatalf("observations are not sorted after clock correction at index %d", i)
		}
	}
}

func TestBitlistLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		buf  []byte
		want uint64
	}{
		{"single byte, length 2, both set (0x07)", []byte{0x07}, 2},
		{"single byte, length 1, sentinel only (0x02)", []byte{0x02}, 1},
		{"single byte, length 8, full (0xff)", []byte{0xff}, 7},
		{"two bytes, sentinel in second byte", []byte{0xff, 0x01}, 8},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := bitlistLength(tc.buf); got != tc.want {
				t.Errorf("bitlistLength(% x) = %d, want %d", tc.buf, got, tc.want)
			}
		})
	}
}

func TestBitSet(t *testing.T) {
	t.Parallel()

	// committee_length=2, both members present: 0b111 = 0x07
	// (data bits 0,1 set, bit 2 is the SSZ Bitlist length sentinel).
	tests := []struct {
		name    string
		hex     string
		index   uint64
		want    bool
		wantErr bool
	}{
		{"both present, check bit 0", "0x07", 0, true, false},
		{"both present, check bit 1", "0x07", 1, true, false},
		{"only bit 0 present: 0b101 = 0x05", "0x05", 0, true, false},
		{"only bit 0 present, bit 1 absent", "0x05", 1, false, false},
		{"index beyond bitlist length", "0x07", 5, false, true},
		{"no leading 0x prefix", "07", 0, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := bitSet(tc.hex, tc.index)
			if (err != nil) != tc.wantErr {
				t.Fatalf("bitSet(%q, %d) error = %v, wantErr %v", tc.hex, tc.index, err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("bitSet(%q, %d) = %v, want %v", tc.hex, tc.index, got, tc.want)
			}
		})
	}
}

func TestAttestationIncludesValidatorElectra(t *testing.T) {
	att := apiAttestation{
		AggregationBits: "0x30",
		CommitteeBits:   "0x0a00000000000000",
	}
	att.Data.Index = "0"
	d := duty{
		CommitteeIndex:          3,
		CommitteeLength:         2,
		CommitteesAtSlot:        4,
		ValidatorCommitteeIndex: 1,
	}

	included, needCommittees, err := attestationIncludesValidator(att, d, nil)
	if err != nil {
		t.Fatalf("attestationIncludesValidator without lengths: %v", err)
	}
	if included || !needCommittees {
		t.Fatalf("included=%t needCommittees=%t, want false true", included, needCommittees)
	}

	included, needCommittees, err = attestationIncludesValidator(att, d, map[uint64]uint64{1: 3, 3: 2})
	if err != nil {
		t.Fatalf("attestationIncludesValidator with lengths: %v", err)
	}
	if !included || needCommittees {
		t.Fatalf("included=%t needCommittees=%t, want true false", included, needCommittees)
	}
}

func TestAttestationIncludesValidatorElectraTargetCommitteeAbsent(t *testing.T) {
	att := apiAttestation{
		AggregationBits: "0x06",
		CommitteeBits:   "0x0100000000000000",
	}
	att.Data.Index = "0"

	included, needCommittees, err := attestationIncludesValidator(att, duty{CommitteeIndex: 1}, nil)
	if err != nil {
		t.Fatalf("attestationIncludesValidator: %v", err)
	}
	if included || needCommittees {
		t.Fatalf("included=%t needCommittees=%t, want false false", included, needCommittees)
	}
}

func TestCgroupIOMaxLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dev         string
		read, write uint64
		want        string
	}{
		{"both unlimited", "254:0", 0, 0, "254:0 rbps=max wbps=max"},
		{"write limited only", "254:0", 0, 1048576, "254:0 rbps=max wbps=1048576"},
		{"both limited", "254:0", 500, 1000, "254:0 rbps=500 wbps=1000"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := cgroupIOMaxLine(tc.dev, tc.read, tc.write); got != tc.want {
				t.Errorf("cgroupIOMaxLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCgroupIOMaxDeviceLine(t *testing.T) {
	t.Parallel()

	content := "8:0 rbps=max wbps=max\n259:0 rbps=1048576 wbps=2097152 riops=20 wiops=30\n"
	got, ok := cgroupIOMaxDeviceLine(content, "259:0")
	if !ok {
		t.Fatal("cgroupIOMaxDeviceLine did not find device")
	}
	want := "259:0 rbps=1048576 wbps=2097152 riops=20 wiops=30"
	if got != want {
		t.Fatalf("cgroupIOMaxDeviceLine = %q, want %q", got, want)
	}
	if _, ok := cgroupIOMaxDeviceLine(content, "7:0"); ok {
		t.Fatal("cgroupIOMaxDeviceLine found absent device")
	}
}

// TestPollBlockSeenWaitsForNodeRecovery covers the failure that aborted a real
// corpus run: a fault aimed at the execution client left the beacon node
// briefly not execution-valid, and sampling that state once at the deadline
// gave up on a node that recovered moments later. The first status read here
// reports el_offline, the second reports a healthy node past the slot, and the
// canonical header stays 404 throughout — so the slot is a confirmed skip.
func TestPollBlockSeenWaitsForNodeRecovery(t *testing.T) {
	var statusReads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eth/v1/node/syncing" {
			http.NotFound(w, r)
			return
		}
		statusReads++
		w.Header().Set("Content-Type", "application/json")
		if statusReads == 1 {
			_, _ = w.Write([]byte(`{"data":{"head_slot":"790","sync_distance":"0","is_syncing":false,"is_optimistic":false,"el_offline":true}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"head_slot":"790","sync_distance":"0","is_syncing":false,"is_optimistic":false,"el_offline":false}}`))
	}))
	t.Cleanup(server.Close)

	observer := &Observer{BeaconAPI: server.URL, Client: server.Client(), NodeRecoveryBudget: time.Minute}
	got, err := observer.PollBlockSeen(context.Background(), 788, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatalf("PollBlockSeen: %v", err)
	}
	if !got.Skipped || got.Found {
		t.Fatalf("status = %+v, want a confirmed skipped slot after the node recovered", got)
	}
	if statusReads < 2 {
		t.Fatalf("status reads = %d, want the degraded state to be retried", statusReads)
	}
}

func TestParsePSISomeAvg10RejectsImpossibleValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"-0.01", "100.01", "NaN", "+Inf"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := parsePSISomeAvg10("some avg10=" + value + " avg60=0 avg300=0 total=0\n"); err == nil {
				t.Fatalf("parsePSISomeAvg10(%q): want error", value)
			}
		})
	}
}
