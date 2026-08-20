package gumble

import "testing"

// Regression: CryptSetup selected UDP before any authenticated packet had
// returned, causing TCP audio to be discarded on blocked inbound UDP paths.
func TestUDPOnlyBecomesActiveAfterAuthenticatedResponse(t *testing.T) {
	c := &Client{}
	if c.udpActive {
		t.Fatal("new transport is unexpectedly active")
	}
	c.markUDPActive()
	c.udpMu.RLock()
	active := c.udpActive
	c.udpMu.RUnlock()
	if !active {
		t.Fatal("authenticated UDP response did not activate UDP")
	}
}
