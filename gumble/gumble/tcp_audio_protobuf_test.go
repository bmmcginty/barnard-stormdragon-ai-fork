package gumble

import (
	"net"
	"testing"
)

// Regression: Mumble 1.5 decodes UDPTunnel packets as native protobuf UDP
// envelopes. Sending the legacy tunnel envelope made a current server silently
// discard otherwise valid Opus audio when UDP was unavailable.
func TestWriteAudioUsesProtobufEnvelopeForTCPFallback(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()
	c := &Client{Config: NewConfig(), Conn: NewConn(local), udpProtobuf: true}
	c.Config.DisableUDP = true
	result := make(chan struct {
		typ  uint16
		data []byte
		err  error
	}, 1)
	go func() {
		typ, data, err := NewConn(remote).ReadPacket()
		result <- struct {
			typ  uint16
			data []byte
			err  error
		}{typ, data, err}
	}()
	if err := c.WriteAudio(4, 2, 300, true, []byte{0xaa, 0xbb}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil || got.typ != 1 {
		t.Fatalf("packet: type=%d err=%v", got.typ, got.err)
	}
	want := mustDecodeHex("00080220ac022a02aabb800101")
	if string(got.data) != string(want) {
		t.Fatalf("payload=%x want=%x", got.data, want)
	}
}
