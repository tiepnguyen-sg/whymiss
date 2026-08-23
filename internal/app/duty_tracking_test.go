package app

import (
	"testing"
	"time"
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
