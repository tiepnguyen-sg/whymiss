package app

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestDutyWindowOpen(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if !dutyWindowOpen(start.Add(35*time.Second), start, 12*time.Second) {
		t.Fatal("window closed before its deadline")
	}
	if dutyWindowOpen(start.Add(36*time.Second), start, 12*time.Second) {
		t.Fatal("window remained open at its deadline")
	}
}

func TestCollectionWindowEnd(t *testing.T) {
	start := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	// Slot 100 is the fifth slot of epoch 3. Deneb permits inclusion through
	// slot 159 (delay 59), plus three slots of canonical-head slack.
	if got, want := collectionWindowEnd(100, start, 12*time.Second), start.Add(62*12*time.Second); !got.Equal(want) {
		t.Fatalf("collectionWindowEnd = %s, want %s", got, want)
	}
}

// A clean shutdown reaches an in-flight collector as a cancelled context. The
// 72-hour release soak logged that at ERROR on its way out, which reads to an
// operator as something having gone wrong at the exact moment nothing did.
func TestCollectionErrorSeparatesShutdownFromFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cancelled bool
		err       error
		wantLevel string
	}{
		{"a real failure is an error", false, errors.New("unexpected status 500"), "ERROR"},
		{"a cancelled context is a shutdown", true, errors.New("unexpected status 500"), "DEBUG"},
		{"a cancellation error is a shutdown", false, fmt.Errorf("poll: %w", context.Canceled), "DEBUG"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			logger, buf := newTestLogger()

			var failed atomic.Bool
			collectionError(ctx, logger, &failed, "poll block_seen", tc.err, domain.Slot(7))

			// The duty is incomplete either way: collection really was cut
			// short, so collection_completed must not be written for it.
			if !failed.Load() {
				t.Error("collectionError did not mark the collection failed")
			}
			lines := logLines(t, buf)
			if len(lines) != 1 {
				t.Fatalf("got %d log lines, want 1", len(lines))
			}
			if got := lines[0]["level"]; got != tc.wantLevel {
				t.Errorf("level = %v, want %v", got, tc.wantLevel)
			}
			if got, ok := lines[0]["slot"].(float64); !ok || got != 7 {
				t.Errorf("slot = %v, want 7", lines[0]["slot"])
			}
		})
	}
}
