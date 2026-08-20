package main

import "testing"

func TestBitlistLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		buf  []byte
		want uint64
	}{
		{"single byte, length 2, both set (0x07)", []byte{0x07}, 2},
		{"single byte, length 1, sentinel only (0x02)", []byte{0x02}, 1},
		{"single byte, length 8, full (0xff)", []byte{0xff}, 7},
		{"two bytes, sentinel in second byte", []byte{0xff, 0x01}, 8},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := bitlistLength(tc.buf); got != tc.want {
				t.Errorf("bitlistLength(% x) = %d, want %d", tc.buf, got, tc.want)
			}
		})
	}
}

func TestBitSet(t *testing.T) {
	t.Parallel()

	// committee_length=2, both members present: 0b111 = 0x07
	// (data bits 0,1 set, bit 2 is the SSZ Bitlist length sentinel).
	tests := []struct {
		name    string
		hex     string
		index   uint64
		want    bool
		wantErr bool
	}{
		{"both present, check bit 0", "0x07", 0, true, false},
		{"both present, check bit 1", "0x07", 1, true, false},
		{"only bit 0 present: 0b101 = 0x05", "0x05", 0, true, false},
		{"only bit 0 present, bit 1 absent", "0x05", 1, false, false},
		{"index beyond bitlist length", "0x07", 5, false, true},
		{"no leading 0x prefix", "07", 0, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := bitSet(tc.hex, tc.index)
			if (err != nil) != tc.wantErr {
				t.Fatalf("bitSet(%q, %d) error = %v, wantErr %v", tc.hex, tc.index, err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("bitSet(%q, %d) = %v, want %v", tc.hex, tc.index, got, tc.want)
			}
		})
	}
}

func TestCgroupIOMaxLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dev         string
		read, write uint64
		want        string
	}{
		{"both unlimited", "254:0", 0, 0, "254:0 rbps=max wbps=max"},
		{"write limited only", "254:0", 0, 1048576, "254:0 rbps=max wbps=1048576"},
		{"both limited", "254:0", 500, 1000, "254:0 rbps=500 wbps=1000"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := cgroupIOMaxLine(tc.dev, tc.read, tc.write); got != tc.want {
				t.Errorf("cgroupIOMaxLine() = %q, want %q", got, tc.want)
			}
		})
	}
}
