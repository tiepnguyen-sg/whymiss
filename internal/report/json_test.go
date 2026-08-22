package report_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/report"
)

func TestJSON_RoundTrips(t *testing.T) {
	v := verdictFor(t, domain.Verdict{
		Slot:       100,
		Outcome:    domain.OutcomeMissed,
		Cause:      domain.CauseProposerMissed,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At:        time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
			Statement: "no block was observed for this slot",
			Source:    domain.SourceDerived,
		}},
	})

	out, err := report.JSON(v)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var got domain.Verdict
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Cause != v.Cause || got.Slot != v.Slot || got.Confidence != v.Confidence {
		t.Errorf("round-tripped verdict = %+v, want %+v", got, v)
	}
}
