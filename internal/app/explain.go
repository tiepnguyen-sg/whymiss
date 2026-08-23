package app

import (
	"context"
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
)

// Explain answers the product's one question for slot: assembles its
// Timeline from whatever whymiss watch has persisted, then runs it through
// the RCA engine. This is what `whymiss <slot>` calls.
func Explain(ctx context.Context, dbPath string, slot domain.Slot, schedule domain.SlotSchedule, cfg rca.Config) (domain.Verdict, error) {
	tl, err := GetTimelineForValidatorSelection(ctx, dbPath, slot, nil, schedule)
	if err != nil {
		return domain.Verdict{}, fmt.Errorf("explain slot %d: %w", slot, err)
	}
	return rca.Analyze(tl, cfg), nil
}

// ExplainForValidator isolates one validator's duty when several configured
// validators were assigned the same slot.
func ExplainForValidator(ctx context.Context, dbPath string, slot domain.Slot, validator domain.ValidatorIndex, schedule domain.SlotSchedule, cfg rca.Config) (domain.Verdict, error) {
	tl, err := GetTimelineForValidatorSelection(ctx, dbPath, slot, &validator, schedule)
	if err != nil {
		return domain.Verdict{}, fmt.Errorf("explain slot %d: %w", slot, err)
	}
	return rca.Analyze(tl, cfg), nil
}
