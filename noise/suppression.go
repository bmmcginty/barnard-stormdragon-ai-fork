package noise

import (
	"math"
	"sync"
)

// Ensure Suppressor implements the NoiseProcessor interface
var _ interface {
	ProcessSamples(samples []int16)
	IsEnabled() bool
} = (*Suppressor)(nil)

// Suppressor handles noise suppression for audio samples
type Suppressor struct {
	mu sync.Mutex

	enabled   bool
	threshold float32

	// High-pass filter state for low-frequency rumble/DC removal.
	prevInput  float32
	prevOutput float32
	hpAlpha    float32

	// Adaptive suppression state.
	envelope        float32
	noiseFloor      float32
	suppressionGain float32
	clickEnergy     float32

	// Tunables.
	envelopeAttack  float32
	envelopeRelease float32
	noiseAttack     float32
	noiseRelease    float32
	gainAttack      float32
	gainRelease     float32
	speechRatio     float32
	clickDecay      float32
	minNoiseFloor   float32
}

// NewSuppressor creates a new noise suppressor
func NewSuppressor() *Suppressor {
	s := &Suppressor{
		enabled:         false,
		threshold:       0.08,
		hpAlpha:         0.995,
		envelopeAttack:  0.18,
		envelopeRelease: 0.02,
		noiseAttack:     0.08,
		noiseRelease:    0.002,
		gainAttack:      0.35,
		gainRelease:     0.02,
		speechRatio:     4.0,
		clickDecay:      0.93,
		minNoiseFloor:   0.0008,
		suppressionGain: 1.0,
	}
	s.resetStateLocked()
	return s
}

// SetEnabled enables or disables noise suppression
func (s *Suppressor) SetEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enabled == enabled {
		return
	}
	s.enabled = enabled
	s.resetStateLocked()
}

// IsEnabled returns whether noise suppression is enabled
func (s *Suppressor) IsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// SetThreshold sets the noise threshold (0.0 to 1.0)
func (s *Suppressor) SetThreshold(threshold float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threshold = clampFloat32(threshold, 0.0, 1.0)
}

// GetThreshold returns the current noise threshold
func (s *Suppressor) GetThreshold() float32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threshold
}

// ProcessSamples applies noise suppression to audio samples
func (s *Suppressor) ProcessSamples(samples []int16) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled || len(samples) == 0 {
		return
	}

	intensity := s.thresholdToIntensity()
	minGain := 1.0 - (0.92 * intensity)
	eps := float32(1e-6)

	for i, sample := range samples {
		floatSample := float32(sample) / 32768.0
		filtered := s.highPassFilterLocked(floatSample)
		absSample := float32(math.Abs(float64(filtered)))

		s.updateEnvelopeLocked(absSample)
		s.updateNoiseFloorLocked()

		snr := s.envelope / (s.noiseFloor + eps)
		voicePresence := clampFloat32((snr-1.0)/(s.speechRatio-1.0), 0.0, 1.0)

		targetGain := minGain + ((1.0 - minGain) * voicePresence)
		targetGain = s.applyTransientSuppressionLocked(absSample, voicePresence, minGain, targetGain)

		s.applyGainSmoothingLocked(targetGain)

		processed := filtered * s.suppressionGain
		processed = clampFloat32(processed, -1.0, 1.0)
		samples[i] = int16(processed * 32767.0)
	}
}

func (s *Suppressor) highPassFilterLocked(input float32) float32 {
	// Simple high-pass filter: y[n] = alpha * (y[n-1] + x[n] - x[n-1])
	output := s.hpAlpha * (s.prevOutput + input - s.prevInput)
	s.prevInput = input
	s.prevOutput = output
	return output
}

func (s *Suppressor) thresholdToIntensity() float32 {
	// Keep lower legacy threshold values meaningful while allowing up to very aggressive suppression.
	return 1.0 - float32(math.Exp(float64(-28.0*clampFloat32(s.threshold, 0.0, 1.0))))
}

func (s *Suppressor) updateEnvelopeLocked(absSample float32) {
	if absSample > s.envelope {
		s.envelope += s.envelopeAttack * (absSample - s.envelope)
	} else {
		s.envelope += s.envelopeRelease * (absSample - s.envelope)
	}
	if s.envelope < s.minNoiseFloor {
		s.envelope = s.minNoiseFloor
	}
}

func (s *Suppressor) updateNoiseFloorLocked() {
	coef := s.noiseRelease
	if s.envelope < s.noiseFloor*2.2 {
		coef = s.noiseAttack
	}
	s.noiseFloor += coef * (s.envelope - s.noiseFloor)
	if s.noiseFloor < s.minNoiseFloor {
		s.noiseFloor = s.minNoiseFloor
	}
}

func (s *Suppressor) applyTransientSuppressionLocked(absSample float32, voicePresence float32, minGain float32, targetGain float32) float32 {
	s.clickEnergy = (s.clickEnergy * s.clickDecay) + (absSample * (1.0 - s.clickDecay))
	transient := absSample - s.clickEnergy
	transientThreshold := 0.04 + (0.08 * (1.0 - voicePresence))
	if transient > transientThreshold && voicePresence < 0.65 {
		clickGain := minGain * 0.55
		if clickGain < targetGain {
			targetGain = clickGain
		}
	}
	return clampFloat32(targetGain, 0.02, 1.0)
}

func (s *Suppressor) applyGainSmoothingLocked(targetGain float32) {
	if targetGain < s.suppressionGain {
		s.suppressionGain += s.gainAttack * (targetGain - s.suppressionGain)
	} else {
		s.suppressionGain += s.gainRelease * (targetGain - s.suppressionGain)
	}
	s.suppressionGain = clampFloat32(s.suppressionGain, 0.02, 1.0)
}

func (s *Suppressor) resetStateLocked() {
	s.prevInput = 0.0
	s.prevOutput = 0.0
	s.envelope = s.minNoiseFloor
	s.noiseFloor = s.minNoiseFloor
	s.suppressionGain = 1.0
	s.clickEnergy = 0.0
}

func clampFloat32(value float32, min float32, max float32) float32 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ProcessSamplesAdvanced applies more sophisticated noise suppression
// Placeholder for future RNNoise integration.
func (s *Suppressor) ProcessSamplesAdvanced(samples []int16) {
	s.ProcessSamples(samples)
}
