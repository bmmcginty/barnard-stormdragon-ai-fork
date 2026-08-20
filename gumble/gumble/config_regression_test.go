package gumble

import (
	"testing"
	"time"
)

// Regression: arbitrary intervals were truncated to 10 ms frames while the
// ticker kept the original duration, producing malformed audio timing.
func TestConfigValidateRejectsUnsupportedAudioInterval(t *testing.T) {
	config := NewConfig()
	config.AudioInterval = 15 * time.Millisecond
	if err := config.Validate(); err == nil {
		t.Fatal("invalid audio interval was accepted")
	}
	config.AudioInterval = 60 * time.Millisecond
	if err := config.Validate(); err != nil {
		t.Fatalf("valid audio interval rejected: %v", err)
	}
}
