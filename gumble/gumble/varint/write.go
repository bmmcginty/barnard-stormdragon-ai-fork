package varint

import (
	"encoding/binary"
	"math"
)

// MaxVarintLen is the maximum number of bytes required to encode a varint
// number.
const MaxVarintLen = 10

// Encode encodes value in the Mumble varint format. It returns zero when b is
// too small, rather than panicking on a caller-provided short buffer.
func Encode(b []byte, value int64) int {
	var encoded [MaxVarintLen]byte
	n := encode(encoded[:], value)
	if n == 0 || len(b) < n {
		return 0
	}
	copy(b, encoded[:n])
	return n
}

func encode(b []byte, value int64) int {
	if value <= -1 && value >= -4 {
		b[0] = 0xFC | byte(^value&0xFF)
		return 1
	}
	if value < 0 {
		b[0] = 0xF8
		// -math.MinInt64 overflows. The decoder intentionally interprets the
		// following signed 64-bit payload as MinInt64 and negates it modulo 2^64.
		if value == math.MinInt64 {
			b[1] = 0xF4
			binary.BigEndian.PutUint64(b[2:], uint64(value))
			return 10
		}
		return 1 + encode(b[1:], -value)
	}
	if value <= 0x7F {
		b[0] = byte(value)
		return 1
	}
	if value <= 0x3FFF {
		b[0] = byte(value>>8)&0x3F | 0x80
		b[1] = byte(value)
		return 2
	}
	if value <= 0x1FFFFF {
		b[0] = byte(value>>16)&0x1F | 0xC0
		b[1], b[2] = byte(value>>8), byte(value)
		return 3
	}
	if value <= 0xFFFFFFF {
		b[0] = byte(value>>24)&0x0F | 0xE0
		b[1], b[2], b[3] = byte(value>>16), byte(value>>8), byte(value)
		return 4
	}
	if value <= math.MaxInt32 {
		b[0] = 0xF0
		binary.BigEndian.PutUint32(b[1:], uint32(value))
		return 5
	}
	b[0] = 0xF4
	binary.BigEndian.PutUint64(b[1:], uint64(value))
	return 9
}
