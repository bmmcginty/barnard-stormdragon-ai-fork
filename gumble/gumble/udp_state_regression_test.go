package gumble

import (
	"testing"
	"time"
)

// Regression: native UDP decoded Users and per-user decoder state while TCP
// handlers concurrently removed users or changed channels. UDP decoding must
// share the client state lock with those handlers.
func TestUDPTunnelWaitsForClientStateLock(t *testing.T) {
	c := &Client{}
	c.volatile.Lock()
	done := make(chan struct{})
	go func() { _ = c.handleUDPTunnel([]byte{0}); close(done) }()
	select {
	case <-done:
		t.Fatal("UDP handler bypassed client state lock")
	case <-time.After(20 * time.Millisecond):
	}
	c.volatile.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("UDP handler did not resume")
	}
}
