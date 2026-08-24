package gumble

import "testing"

// Regression: Buffers had no upper bound, but it is allocated per speaking
// user both as a decoded-frame queue and as OpenAL playback buffers.
func TestConfigRejectsOversizedBuffers(t *testing.T) {
	config := NewConfig()
	config.Buffers = MaximumBuffers + 1
	if err := config.Validate(); err == nil {
		t.Fatal("expected Buffers above the maximum to be rejected")
	}
	config.Buffers = MaximumBuffers
	if err := config.Validate(); err != nil {
		t.Fatalf("Buffers at the maximum should be accepted: %v", err)
	}
}
