package beaconapi

import "testing"

// Cases drawn from real aggregation_bits values captured off a live devnet
// (testdata/pool_attestations.json, testdata/block.json), not invented.
func TestBitSet(t *testing.T) {
	tests := []struct {
		name    string
		hexBits string
		index   uint64
		want    bool
	}{
		{"0x07 bit 0 set", "0x07", 0, true},
		{"0x07 bit 1 set", "0x07", 1, true},
		{"0x05 bit 0 set", "0x05", 0, true},
		{"0x05 bit 1 not set", "0x05", 1, false},
		{"0x06 bit 0 not set", "0x06", 0, false},
		{"0x06 bit 1 set", "0x06", 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bitSet(tt.hexBits, tt.index)
			if err != nil {
				t.Fatalf("bitSet(%q, %d): %v", tt.hexBits, tt.index, err)
			}
			if got != tt.want {
				t.Errorf("bitSet(%q, %d) = %v, want %v", tt.hexBits, tt.index, got, tt.want)
			}
		})
	}
}

func TestBitSet_OutOfRange(t *testing.T) {
	if _, err := bitSet("0x07", 2); err == nil {
		t.Fatal("bitSet: want error for the length-sentinel bit itself, got nil")
	}
}
