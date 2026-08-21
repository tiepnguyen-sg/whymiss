package app

import (
	"context"
	"fmt"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/rca"
)

// Explain answers the product's one question for slot: assembles its
// Timeline from whatever whymiss watch has persisted, then runs it through
// the RCA engine. This is what `whymiss <slot>` calls.
func Explain(ctx context.Context, dbPath string, slot domain.Slot, schedule domain.SlotSchedule) (domain.Verdict, error) {
	tl, err := GetTimeline(ctx, dbPath, slot, schedule)
	if err != nil {
		return domain.Verdict{}, fmt.Errorf("explain slot %d: %w", slot, err)
	}
	return rca.Analyze(tl, rca.DefaultConfig()), nil
}
