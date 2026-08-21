package hostmetrics

import (
	"os"
	"path/filepath"
	"testing"
)

// statFixture follows /proc/stat's documented format (man 5 proc): "cpu"
// plus ten cumulative jiffie counters — user nice system idle iowait irq
// softirq steal guest guest_nice. A kernel ABI, not a client response.
func writeStatFixture(t *testing.T, steal, otherTotal uint64) string {
	t.Helper()
	// user=otherTotal, rest zero except steal, so total = otherTotal + steal.
	content := "cpu  " + itoa(otherTotal) + " 0 0 0 0 0 0 " + itoa(steal) + " 0 0\n"
	path := filepath.Join(t.TempDir(), "stat")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func TestCPUSteal_FirstSampleNotOK(t *testing.T) {
	restore := statPath
	statPath = writeStatFixture(t, 100, 900)
	defer func() { statPath = restore }()

	var c CPUSteal
	_, ok, err := c.Sample()
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if ok {
		t.Error("Sample: want ok=false on the first call (no prior reading to delta against)")
	}
}

func TestCPUSteal_SecondSampleComputesDelta(t *testing.T) {
	restore := statPath
	defer func() { statPath = restore }()

	var c CPUSteal
	statPath = writeStatFixture(t, 100, 900) // total=1000, steal=100
	if _, _, err := c.Sample(); err != nil {
		t.Fatalf("Sample (first): %v", err)
	}

	statPath = writeStatFixture(t, 150, 1850) // total=2000, steal=150: delta steal=50, delta total=1000 -> 5%
	got, ok, err := c.Sample()
	if err != nil {
		t.Fatalf("Sample (second): %v", err)
	}
	if !ok {
		t.Fatal("Sample: want ok=true on the second call")
	}
	if got.Value != 5 {
		t.Errorf("Value = %v, want 5 (50/1000 * 100)", got.Value)
	}
	if got.Name != MetricCPUStealPct {
		t.Errorf("Name = %q, want %q", got.Name, MetricCPUStealPct)
	}
}

func TestCPUSteal_Unavailable(t *testing.T) {
	restore := statPath
	statPath = filepath.Join(t.TempDir(), "does-not-exist")
	defer func() { statPath = restore }()

	var c CPUSteal
	if _, _, err := c.Sample(); err == nil {
		t.Error("Sample: want an error when /proc/stat is absent, got nil")
	}
}
