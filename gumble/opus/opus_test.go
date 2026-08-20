package opus

import "testing"

// Regression: the encoder always assumed ten millisecond frames, causing the
// bitrate for 20/40/60 ms packets to be 2/4/6 times their actual budget.
func TestEncoderBitrateUsesFrameDuration(t *testing.T) {
	const budget = 100
	for _, frameSamples := range []int{480, 960, 1920, 2880} {
		got := encoderBitrate(budget, frameSamples, 1)
		want := budget * 8 * 48000 / frameSamples
		if got != want {
			t.Fatalf("%d samples: got %d, want %d", frameSamples, got, want)
		}
	}
}
