package noise

import (
	"math"
)

// Ensure Suppressor implements the NoiseProcessor interface
var _ interface {
	ProcessSamples(samples []int16)
	IsEnabled() bool
} = (*Suppressor)(nil)

// Suppressor handles noise suppression for audio samples
type Suppressor struct {
	enabled    bool
	threshold  float32
	gainFactor float32
	
	// Simple high-pass filter state for DC removal
	prevInput  float32
	prevOutput float32
	alpha      float32
}

// NewSuppressor creates a new noise suppressor
func NewSuppressor() *Suppressor {
	return &Suppressor{
		enabled:    false,
		threshold:  0.02,     // Noise threshold level
		gainFactor: 0.8,      // Gain reduction for noise
		alpha:      0.95,     // High-pass filter coefficient
	}
}

// SetEnabled enables or disables noise suppression
func (s *Suppressor) SetEnabled(enabled bool) {
	s.enabled = enabled
}

// IsEnabled returns whether noise suppression is enabled
func (s *Suppressor) IsEnabled() bool {
	return s.enabled
}

// SetThreshold sets the noise threshold (0.0 to 1.0)
func (s *Suppressor) SetThreshold(threshold float32) {
	if threshold >= 0.0 && threshold <= 1.0 {
		s.threshold = threshold
	}
}

// GetThreshold returns the current noise threshold
func (s *Suppressor) GetThreshold() float32 {
	return s.threshold
}

// ProcessSamples applies noise suppression to audio samples
func (s *Suppressor) ProcessSamples(samples []int16) {
	if !s.enabled || len(samples) == 0 {
		return
	}

	// Simple noise suppression algorithm
	for i, sample := range samples {
		// Convert to float for processing
		floatSample := float32(sample) / 32767.0
		
		// Apply high-pass filter for DC removal
		filtered := s.highPassFilter(floatSample)
		
		// Calculate signal strength
		strength := float32(math.Abs(float64(filtered)))
		
		// Apply noise gate
		if strength < s.threshold {
			// Below threshold - reduce gain
			filtered *= s.gainFactor
		}
		
		// Apply simple spectral subtraction-like effect
		// If signal is weak, reduce it further
		if strength < s.threshold * 2 {
			filtered *= (strength / (s.threshold * 2))
		}
		
		// Convert back to int16
		processed := filtered * 32767.0
		if processed > 32767 {
			processed = 32767
		} else if processed < -32767 {
			processed = -32767
		}
		
		samples[i] = int16(processed)
	}
}

// highPassFilter applies a simple high-pass filter to remove DC component
func (s *Suppressor) highPassFilter(input float32) float32 {
	// Simple high-pass filter: y[n] = alpha * (y[n-1] + x[n] - x[n-1])
	output := s.alpha * (s.prevOutput + input - s.prevInput)
	s.prevInput = input
	s.prevOutput = output
	return output
}

// ProcessSamplesAdvanced applies more sophisticated noise suppression
// This is a placeholder for future RNNoise integration
func (s *Suppressor) ProcessSamplesAdvanced(samples []int16) {
	// TODO: Integrate RNNoise or other advanced algorithms
	s.ProcessSamples(samples)
}