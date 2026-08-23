package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

func TestDoctorRejectsIncompleteConfigWithoutNetworkIO(t *testing.T) {
	checks := Doctor(context.Background(), DoctorConfig{})
	if len(checks) != 1 || checks[0].Name != "config" || checks[0].Err == nil {
		t.Fatalf("checks = %+v, want one failed config check", checks)
	}
}

func TestCheckDBPath(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "new.db")
	if err := checkDBPath(context.Background(), newPath); err != nil {
		t.Fatalf("new path: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("doctor created database %s", newPath)
	}

	existing := filepath.Join(dir, "existing.db")
	st, err := store.Open(context.Background(), existing)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := checkDBPath(context.Background(), existing); err != nil {
		t.Fatalf("existing path: %v", err)
	}
	if err := checkDBPath(context.Background(), dir); err == nil {
		t.Error("directory path: want error")
	}

	corrupt := filepath.Join(dir, "corrupt.db")
	if err := os.WriteFile(corrupt, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkDBPath(context.Background(), corrupt); err == nil {
		t.Error("corrupt database: want error")
	}
}

func TestDoctorRejectsInvalidBeaconURLBeforeNetworkIO(t *testing.T) {
	checks := Doctor(context.Background(), DoctorConfig{
		BeaconAPI:          "://invalid",
		DBPath:             filepath.Join(t.TempDir(), "whymiss.db"),
		MinRequestInterval: time.Millisecond,
		ClockOffsetMax:     100 * time.Millisecond,
	})
	if len(checks) != 1 || checks[0].Name != "config" || checks[0].Err == nil {
		t.Fatalf("checks = %+v, want one failed config check", checks)
	}
}
