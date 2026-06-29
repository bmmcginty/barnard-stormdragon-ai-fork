package noise

import "testing"

func TestSuppressorDisabledBypassesSamples(t *testing.T) {
	suppressor := NewSuppressor()
	defer suppressor.Close()

	samples := []int16{100, -200, 300, -400, 500}
	original := append([]int16(nil), samples...)

	suppressor.ProcessSamples(samples)

	for i := range samples {
		if samples[i] != original[i] {
			t.Fatalf("expected sample %d to remain unchanged, got %d want %d", i, samples[i], original[i])
		}
	}
}

func TestSuppressorProcessesCompleteRNNoiseFrame(t *testing.T) {
	suppressor := NewSuppressor()
	defer suppressor.Close()
	suppressor.SetEnabled(true)

	samples := make([]int16, suppressor.frameSize)
	for i := range samples {
		samples[i] = int16((i % 64) * 128)
	}

	suppressor.ProcessSamples(samples)
}

func TestSuppressorLeavesIncompleteFrameUnchanged(t *testing.T) {
	suppressor := NewSuppressor()
	defer suppressor.Close()
	suppressor.SetEnabled(true)

	samples := make([]int16, suppressor.frameSize-1)
	for i := range samples {
		samples[i] = int16(i + 1)
	}
	original := append([]int16(nil), samples...)

	suppressor.ProcessSamples(samples)

	for i := range samples {
		if samples[i] != original[i] {
			t.Fatalf("expected incomplete frame sample %d to remain unchanged, got %d want %d", i, samples[i], original[i])
		}
	}
}

func TestSuppressorCloseIsIdempotent(t *testing.T) {
	suppressor := NewSuppressor()
	suppressor.Close()
	suppressor.Close()
}
