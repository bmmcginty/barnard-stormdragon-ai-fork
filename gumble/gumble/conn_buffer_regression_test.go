package gumble

import (
	"encoding/binary"
	"net"
	"testing"
)

// Regression: the read buffer grew to the largest packet ever seen and kept
// that memory for the life of the connection.
func TestConnBufferShrinksAfterAnOversizedPacket(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	conn := NewConn(client)

	big := 4 * 1024 * 1024
	go func() {
		defer server.Close()
		writePacket := func(length int) {
			var header [6]byte
			binary.BigEndian.PutUint16(header[:], 3)
			binary.BigEndian.PutUint32(header[2:], uint32(length))
			server.Write(header[:])
			server.Write(make([]byte, length))
		}
		writePacket(big)
		writePacket(128)
	}()

	if _, _, err := conn.ReadPacket(); err != nil {
		t.Fatalf("reading the oversized packet: %v", err)
	}
	if len(conn.buffer) < big {
		t.Fatalf("oversized packet should have grown the buffer, got %d", len(conn.buffer))
	}
	if _, _, err := conn.ReadPacket(); err != nil {
		t.Fatalf("reading the small packet: %v", err)
	}
	if len(conn.buffer) > retainedPacketBytes {
		t.Fatalf("buffer stayed at %d bytes after a small packet, above the %d retained size",
			len(conn.buffer), retainedPacketBytes)
	}
}
