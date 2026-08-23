package rules

import (
	"math"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestComputeStages(t *testing.T) {
	t.Run("computes ordered stage boundaries", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			mustObs(t, domain.ObsHeadUpdated, offset(2*time.Second), nil),
			mustObs(t, domain.ObsAttestationPublished, offset(3*time.Second), nil),
		)
		got := ComputeStages(tl)
		if !got.HasPropagation || got.Propagation != time.Second || !got.HasValidation || got.Validation != time.Second || !got.HasSigning || got.Signing != time.Second {
			t.Fatalf("stages = %+v", got)
		}
	})

	t.Run("rejects a block timestamp before slot start", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(-time.Millisecond), nil))
		if got := ComputeStages(tl); got.HasPropagation || got.HasValidation || got.HasSigning {
			t.Fatalf("stages = %+v, want no timing stages", got)
		}
	})

	t.Run("does not create negative validation or signing durations", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsHeadUpdated, offset(time.Second), nil),
			mustObs(t, domain.ObsBlockSeen, offset(2*time.Second), nil),
			mustObs(t, domain.ObsAttestationPublished, offset(3*time.Second), nil),
		)
		got := ComputeStages(tl)
		if got.HasValidation || got.HasSigning {
			t.Fatalf("stages = %+v, want invalid downstream stages omitted", got)
		}
	})
}

func TestStagesTotalSaturates(t *testing.T) {
	stages := Stages{
		Propagation: time.Duration(math.MaxInt64), Validation: time.Second,
		HasPropagation: true, HasValidation: true,
	}
	if got := stages.Total(); got != time.Duration(math.MaxInt64) {
		t.Fatalf("Total = %s, want saturated MaxInt64", got)
	}
}
