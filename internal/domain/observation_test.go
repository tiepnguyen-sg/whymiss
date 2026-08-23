package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// at is a fixed instant used across the domain tests. A constant rather than
// time.Now keeps every assertion reproducible, which is the property the whole
// engine rests on (I-6).
var at = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

var (
	rootA = "0x" + strings.Repeat("a", 64)
	rootB = "0x" + strings.Repeat("b", 64)
)

func TestNewObservationValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		draft domain.Observation
	}{
		{
			name: "minimal, no attributes",
			draft: domain.Observation{
				Slot:   100,
				Kind:   domain.ObsSlotStart,
				At:     at,
				Source: domain.SourceDerived,
			},
		},
		{
			name: "block seen with permitted attributes",
			draft: domain.Observation{
				Slot:   100,
				Kind:   domain.ObsBlockSeen,
				At:     at,
				Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{
					domain.AttrBlockRoot:     rootA,
					domain.AttrProposerIndex: "123456",
				},
			},
		},
		{
			name: "clock sample carries a negative offset",
			draft: domain.Observation{
				Slot:          100,
				Kind:          domain.ObsClockSampled,
				At:            at,
				ClockOffset:   -42 * time.Millisecond,
				ClockMeasured: true,
				ClockSampleAt: at,
				Source:        domain.SourceClock,
				Attrs:         map[domain.AttrKey]string{domain.AttrValue: "-42"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.NewObservation(tc.draft)
			if err != nil {
				t.Fatalf("NewObservation() error = %v, want nil", err)
			}
			if got.Kind != tc.draft.Kind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.draft.Kind)
			}
			if !got.At.Equal(tc.draft.At) {
				t.Errorf("At = %v, want %v", got.At, tc.draft.At)
			}
		})
	}
}

func TestNewObservationRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		draft domain.Observation
		want  error
	}{
		{
			name: "kind outside the vocabulary",
			draft: domain.Observation{
				Kind: "block_arrived", At: at, Source: domain.SourceBeaconAPI,
			},
			want: domain.ErrInvalidKind,
		},
		{
			name:  "zero timestamp",
			draft: domain.Observation{Kind: domain.ObsBlockSeen, Source: domain.SourceBeaconAPI},
			want:  domain.ErrMissingTimestamp,
		},
		{
			name: "timestamp not in utc",
			draft: domain.Observation{
				Kind:   domain.ObsBlockSeen,
				At:     at.In(time.FixedZone("CET", 3600)),
				Source: domain.SourceBeaconAPI,
			},
			want: domain.ErrNotUTC,
		},
		{
			name:  "empty source",
			draft: domain.Observation{Kind: domain.ObsBlockSeen, At: at},
			want:  domain.ErrMissingSource,
		},
		{
			name: "source outside the adapter set",
			draft: domain.Observation{
				Kind: domain.ObsBlockSeen, At: at, Source: "unregistered_adapter",
			},
			want: domain.ErrMissingSource,
		},
		{
			name: "measured clock without sample timestamp",
			draft: domain.Observation{
				Kind: domain.ObsBlockSeen, At: at, Source: domain.SourceBeaconAPI,
				ClockMeasured: true,
			},
			want: domain.ErrInvalidClock,
		},
		{
			name: "unmeasured observation with clock offset",
			draft: domain.Observation{
				Kind: domain.ObsBlockSeen, At: at, Source: domain.SourceBeaconAPI,
				ClockOffset: time.Millisecond,
			},
			want: domain.ErrInvalidClock,
		},
		{
			name: "undocumented attribute key",
			draft: domain.Observation{
				Kind: domain.ObsBlockSeen, At: at, Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{"graffiti": "hello"},
			},
			want: domain.ErrInvalidAttr,
		},
		{
			name: "documented key on the wrong kind",
			draft: domain.Observation{
				Kind: domain.ObsBlockSeen, At: at, Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{domain.AttrPeerCount: "62"},
			},
			want: domain.ErrInvalidAttr,
		},
		{
			name: "known source on the wrong kind",
			draft: domain.Observation{
				Kind: domain.ObsHostSampled, At: at, Source: domain.SourceBeaconAPI,
			},
			want: domain.ErrInvalidSource,
		},
		{
			name: "empty attribute value",
			draft: domain.Observation{
				Kind: domain.ObsBlockSeen, At: at, Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{domain.AttrBlockRoot: ""},
			},
			want: domain.ErrInvalidAttr,
		},
		{
			name: "oversized attribute value",
			draft: domain.Observation{
				Kind: domain.ObsBlockSeen, At: at, Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{domain.AttrBlockRoot: strings.Repeat("a", 257)},
			},
			want: domain.ErrInvalidAttr,
		},
		{
			name: "malformed block root",
			draft: domain.Observation{
				Kind: domain.ObsBlockSeen, At: at, Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{domain.AttrBlockRoot: "0xabc"},
			},
			want: domain.ErrInvalidAttr,
		},
		{
			name: "nan peer count",
			draft: domain.Observation{
				Kind: domain.ObsPeerCountSampled, At: at, Source: domain.SourcePromScrape,
				Attrs: map[domain.AttrKey]string{domain.AttrPeerCount: "NaN"},
			},
			want: domain.ErrInvalidAttr,
		},
		{
			name: "negative peer count",
			draft: domain.Observation{
				Kind: domain.ObsPeerCountSampled, At: at, Source: domain.SourcePromScrape,
				Attrs: map[domain.AttrKey]string{domain.AttrPeerCount: "-1"},
			},
			want: domain.ErrInvalidAttr,
		},
		{
			name: "infinite host value",
			draft: domain.Observation{
				Kind: domain.ObsHostSampled, At: at, Source: domain.SourceHostMetrics,
				Attrs: map[domain.AttrKey]string{domain.AttrMetric: "iowait_pct", domain.AttrValue: "+Inf"},
			},
			want: domain.ErrInvalidAttr,
		},
		{
			name: "nan engine duration",
			draft: domain.Observation{
				Kind: domain.ObsEngineCall, At: at, Source: domain.SourcePromScrape,
				Attrs: map[domain.AttrKey]string{
					domain.AttrEngineMethod: domain.EngineMethodNewPayload,
					domain.AttrDurationMS:   "NaN",
				},
			},
			want: domain.ErrInvalidAttr,
		},
		{
			name: "zero baseline sample count",
			draft: domain.Observation{
				Kind: domain.ObsNetworkBaselineSampled, At: at, Source: domain.SourceXatu,
				Attrs: map[domain.AttrKey]string{
					domain.AttrBlockArrivalP50MS: "100",
					domain.AttrBlockArrivalP90MS: "200",
					domain.AttrSampleCount:       "0",
				},
			},
			want: domain.ErrInvalidAttr,
		},
		{
			name: "unknown engine method",
			draft: domain.Observation{
				Kind: domain.ObsEngineCall, At: at, Source: domain.SourcePromScrape,
				Attrs: map[domain.AttrKey]string{
					domain.AttrEngineMethod: "engine_exchangeCapabilities",
					domain.AttrDurationMS:   "1",
				},
			},
			want: domain.ErrInvalidAttr,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := domain.NewObservation(tc.draft)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewObservation() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestNewObservationCopiesAttrs proves the immutability the type documents: an
// adapter that reuses its attribute map between observations must not be able to
// rewrite history already on a timeline.
func TestNewObservationCopiesAttrs(t *testing.T) {
	t.Parallel()

	attrs := map[domain.AttrKey]string{domain.AttrBlockRoot: rootA}
	obs, err := domain.NewObservation(domain.Observation{
		Slot: 1, Kind: domain.ObsBlockSeen, At: at,
		Source: domain.SourceBeaconAPI, Attrs: attrs,
	})
	if err != nil {
		t.Fatalf("NewObservation() error = %v", err)
	}

	attrs[domain.AttrBlockRoot] = rootB

	if got, _ := obs.Attr(domain.AttrBlockRoot); got != rootA {
		t.Errorf("block root = %q after caller mutated its map, want %q", got, rootA)
	}
}

func TestNewObservationCanonicalizesBlockRootCase(t *testing.T) {
	t.Parallel()
	obs, err := domain.NewObservation(domain.Observation{
		Slot: 1, Kind: domain.ObsBlockSeen, At: at, Source: domain.SourceBeaconAPI,
		Attrs: map[domain.AttrKey]string{domain.AttrBlockRoot: "0x" + strings.Repeat("A", 64)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := obs.Attr(domain.AttrBlockRoot); got != rootA {
		t.Fatalf("block root = %q, want canonical %q", got, rootA)
	}
}

func TestObservationOffset(t *testing.T) {
	t.Parallel()

	obs := domain.Observation{At: at.Add(3900 * time.Millisecond)}
	if got, want := obs.Offset(at), 3900*time.Millisecond; got != want {
		t.Errorf("Offset() = %s, want %s", got, want)
	}

	early := domain.Observation{At: at.Add(-2 * time.Second)}
	if got := early.Offset(at); got >= 0 {
		t.Errorf("Offset() = %s for a fact before the slot, want negative", got)
	}
}

func TestAttrKeyPermittedFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  domain.AttrKey
		kind domain.ObservationKind
		want bool
	}{
		{domain.AttrBlockRoot, domain.ObsBlockSeen, true},
		{domain.AttrBlockRoot, domain.ObsHeadUpdated, true},
		{domain.AttrProposerIndex, domain.ObsBlockSeen, true},
		{domain.AttrProposerIndex, domain.ObsHeadUpdated, false},
		{domain.AttrInclusionDelay, domain.ObsAttestationIncluded, true},
		{domain.AttrInclusionDelay, domain.ObsAttestationPublished, false},
		{domain.AttrHeadCorrect, domain.ObsAttestationIncluded, true},
		{domain.AttrTargetCorrect, domain.ObsAttestationPublished, false},
		{domain.AttrValue, domain.ObsClockSampled, true},
		{domain.AttrValue, domain.ObsHostSampled, true},
		{domain.AttrPeerCount, domain.ObsPeerCountSampled, true},
		{domain.AttrBlockRoot, "not_a_kind", false},
		{"not_a_key", domain.ObsBlockSeen, false},
	}

	for _, tc := range tests {
		t.Run(string(tc.key)+"/"+string(tc.kind), func(t *testing.T) {
			t.Parallel()

			if got := tc.key.PermittedFor(tc.kind); got != tc.want {
				t.Errorf("PermittedFor() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestObservationRejectsInvalidRewardEvidenceBoolean(t *testing.T) {
	_, err := domain.NewObservation(domain.Observation{
		Slot: 1, Kind: domain.ObsAttestationIncluded, At: at, Source: domain.SourceBeaconAPI,
		Attrs: map[domain.AttrKey]string{domain.AttrHeadCorrect: "yes"},
	})
	if err == nil {
		t.Fatal("NewObservation: want invalid boolean error, got nil")
	}
}

// TestObservationKindsIsACopy guards the closed vocabulary against a caller that
// sorts or truncates the slice it was handed.
func TestObservationKindsIsACopy(t *testing.T) {
	t.Parallel()

	kinds := domain.ObservationKinds()
	if len(kinds) == 0 {
		t.Fatal("ObservationKinds() is empty")
	}
	kinds[0] = "clobbered"

	if domain.ObservationKinds()[0] == "clobbered" {
		t.Error("ObservationKinds() exposed its backing array")
	}
	if !domain.ObsSlotStart.Valid() {
		t.Error("slot_start stopped being valid after a caller mutated the returned slice")
	}
}

func TestAttrKeys(t *testing.T) {
	t.Parallel()

	keys := domain.AttrKeys()
	if len(keys) == 0 {
		t.Fatal("AttrKeys() is empty")
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Fatalf("AttrKeys() not sorted: %q >= %q", keys[i-1], keys[i])
		}
	}

	found := false
	for _, k := range keys {
		if k == domain.AttrBlockRoot {
			found = true
		}
	}
	if !found {
		t.Error("AttrKeys() missing block_root")
	}
}
