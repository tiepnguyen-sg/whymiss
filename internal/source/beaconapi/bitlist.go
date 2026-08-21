package beaconapi

import (
	"fmt"
	"math/bits"
	"strings"
)

// bitSet reports whether index is set in hexBits, an SSZ Bitlist as the
// Beacon API encodes it (a 0x-prefixed hex string).
func bitSet(hexBits string, index uint64) (bool, error) {
	raw := strings.TrimPrefix(hexBits, "0x")
	if len(raw)%2 != 0 {
		raw = "0" + raw
	}
	buf := make([]byte, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		var b uint64
		if _, err := fmt.Sscanf(raw[i:i+2], "%02x", &b); err != nil {
			return false, fmt.Errorf("decode hex byte %q: %w", raw[i:i+2], err)
		}
		buf = append(buf, byte(b))
	}

	length := bitlistLength(buf)
	if index >= length {
		return false, fmt.Errorf("index %d out of range for bitlist of length %d", index, length)
	}
	byteIdx, bitIdx := index/8, index%8
	return buf[byteIdx]&(1<<bitIdx) != 0, nil
}

// bitlistLength returns the number of data bits in an SSZ Bitlist encoding:
// the position of the highest set bit across the whole byte slice — that bit
// is a length sentinel, not data, per the SSZ Bitlist spec.
func bitlistLength(buf []byte) uint64 {
	for i := len(buf) - 1; i >= 0; i-- {
		if buf[i] == 0 {
			continue
		}
		topBit := bits.Len8(buf[i]) - 1
		//nolint:gosec // G115: i is a bounded slice index and topBit is in [0,7]; neither can be negative here
		return uint64(i)*8 + uint64(topBit)
	}
	return 0
}
