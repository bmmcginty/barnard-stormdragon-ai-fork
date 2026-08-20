package gumble

import (
	"net"
	"testing"
	"time"
)

// Regression: negative durations were converted to uint32 seconds for the
// protocol, creating an unexpectedly huge ban rather than a safe duration.
func TestBanDurationsNeverRemainNegative(t *testing.T) {
	var bans BanList
	ban := bans.Add(net.ParseIP("192.0.2.1"), net.CIDRMask(32, 32), "test", -time.Minute)
	if ban.Duration != 0 {
		t.Fatalf("Add duration = %v", ban.Duration)
	}
	ban.SetDuration(-time.Second)
	if ban.Duration != 0 {
		t.Fatalf("SetDuration = %v", ban.Duration)
	}
}
