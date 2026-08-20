package varint // import "git.stormux.org/storm/barnard/gumble/gumble/varint"

import (
	"math"
	"testing"
)

// Regression: MinInt64 formerly caused unbounded recursive encoding, and a
// caller-provided short buffer caused an index panic.
func TestEncodeMinInt64AndShortBuffer(t *testing.T) {
	buf := make([]byte, MaxVarintLen)
	n := Encode(buf, math.MinInt64)
	if n != MaxVarintLen {
		t.Fatalf("length = %d, want %d", n, MaxVarintLen)
	}
	got, consumed := Decode(buf[:n])
	if consumed != n || got != math.MinInt64 {
		t.Fatalf("decoded (%d, %d)", got, consumed)
	}
	if n := Encode(make([]byte, 1), 128); n != 0 {
		t.Fatalf("short buffer returned %d", n)
	}
}

func TestRange(t *testing.T) {

	fn := func(i int64) {
		var b [MaxVarintLen]byte
		size := Encode(b[:], i)
		if size == 0 {
			t.Error("Encode returned size 0\n")
		}
		s := b[:size]

		val, size := Decode(s)
		if size == 0 {
			t.Error("Decode return size 0\n")
		}

		if i != val {
			t.Errorf("Encoded %d (%v) equals decoded %d\n", i, s, val)
		}
	}

	for i := int64(-10000); i <= 10000; i++ {
		fn(i)
	}

	fn(134342525)
	fn(10282934828342)
	fn(1028293482834200000)
}
