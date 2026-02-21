package noise

import (
	"math"
	"testing"
)

func TestSuppressorDisabledBypassesSamples(t *testing.T) {
	suppressor := NewSuppressor()
	samples := []int16{100, -200, 300, -400, 500}
	original := append([]int16(nil), samples...)

	suppressor.ProcessSamples(samples)

	for i := range samples {
		if samples[i] != original[i] {
			t.Fatalf("expected sample %d to remain unchanged, got %d want %d", i, samples[i], original[i])
		}
	}
}

func TestSuppressorAttenuatesLowLevelNoise(t *testing.T) {
	suppressor := NewSuppressor()
	suppressor.SetEnabled(true)
	suppressor.SetThreshold(0.08)

	input := makeSineFrame(600, 700)
	originalRMS := frameRMS(input)
	processed := append([]int16(nil), input...)
	suppressor.ProcessSamples(processed)
	processedRMS := frameRMS(processed)

	if processedRMS >= originalRMS*0.8 {
		t.Fatalf("expected low-level noise attenuation, got RMS %.2f from %.2f", processedRMS, originalRMS)
	}
}

func TestSuppressorPreservesSpeechLikeSignal(t *testing.T) {
	suppressor := NewSuppressor()
	suppressor.SetEnabled(true)
	suppressor.SetThreshold(0.08)

	voice := makeSineFrame(1000, 9000)
	originalRMS := frameRMS(voice)
	processed := append([]int16(nil), voice...)
	suppressor.ProcessSamples(processed)
	processedRMS := frameRMS(processed)

	if processedRMS <= originalRMS*0.6 {
		t.Fatalf("expected speech-like signal to be mostly preserved, got RMS %.2f from %.2f", processedRMS, originalRMS)
	}
}

func TestHigherThresholdAppliesStrongerSuppression(t *testing.T) {
	lowSuppressor := NewSuppressor()
	lowSuppressor.SetEnabled(true)
	lowSuppressor.SetThreshold(0.02)

	highSuppressor := NewSuppressor()
	highSuppressor.SetEnabled(true)
	highSuppressor.SetThreshold(0.20)

	noiseFrame := makeSineFrame(500, 700)
	lowRMS := runFrameWarmup(lowSuppressor, noiseFrame, 8)
	highRMS := runFrameWarmup(highSuppressor, noiseFrame, 8)

	if highRMS >= lowRMS*0.8 {
		t.Fatalf("expected stronger suppression at higher threshold, got low %.2f high %.2f", lowRMS, highRMS)
	}
}

func runFrameWarmup(suppressor *Suppressor, frame []int16, repeats int) float64 {
	var processed []int16
	for i := 0; i < repeats; i++ {
		processed = append([]int16(nil), frame...)
		suppressor.ProcessSamples(processed)
	}
	return frameRMS(processed)
}

func makeSineFrame(frequency float64, amplitude float64) []int16 {
	const sampleRate = 48000.0
	const frameSize = 480

	frame := make([]int16, frameSize)
	for i := 0; i < frameSize; i++ {
		value := math.Sin((2.0 * math.Pi * frequency * float64(i)) / sampleRate)
		frame[i] = int16(value * amplitude)
	}
	return frame
}

func frameRMS(samples []int16) float64 {
	if len(samples) == 0 {
		return 0.0
	}
	var sumSquares float64
	for _, sample := range samples {
		normalized := float64(sample) / 32768.0
		sumSquares += normalized * normalized
	}
	return math.Sqrt(sumSquares / float64(len(samples)))
}
