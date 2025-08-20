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
	
	// Click detection state
	clickThreshold    float32
	clickDecay        float32
	recentClickEnergy float32
}

// NewSuppressor creates a new noise suppressor
func NewSuppressor() *Suppressor {
	return &Suppressor{
		enabled:           false,
		threshold:         0.01,     // Reduced noise threshold level for less aggressive filtering
		gainFactor:        0.9,      // Less aggressive gain reduction for noise
		alpha:             0.98,     // More stable high-pass filter coefficient
		clickThreshold:    0.15,     // Threshold for detecting keyboard clicks
		clickDecay:        0.95,     // How quickly click energy decays
		recentClickEnergy: 0.0,      // Tracks recent click activity
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

	// Calculate frame energy for click detection
	var frameEnergy float32 = 0.0
	for _, sample := range samples {
		floatSample := float32(sample) / 32767.0
		frameEnergy += floatSample * floatSample
	}
	frameEnergy = float32(math.Sqrt(float64(frameEnergy / float32(len(samples)))))
	
	// Detect sudden energy spikes (likely keyboard clicks)
	energySpike := frameEnergy - s.recentClickEnergy
	isClick := energySpike > s.clickThreshold && frameEnergy > 0.05
	
	// Update recent click energy with decay
	s.recentClickEnergy = s.recentClickEnergy*s.clickDecay + frameEnergy*(1.0-s.clickDecay)
	
	// Improved noise suppression algorithm
	for i, sample := range samples {
		// Convert to float for processing
		floatSample := float32(sample) / 32767.0
		
		// Apply high-pass filter for DC removal
		filtered := s.highPassFilter(floatSample)
		
		// Calculate signal strength (RMS-like)
		strength := float32(math.Abs(float64(filtered)))
		
		// Apply noise gate with smooth transition
		var gainReduction float32 = 1.0
		
		// If we detected a click, apply stronger suppression
		if isClick {
			gainReduction = s.gainFactor * 0.3 // Much stronger reduction for clicks
		} else if strength < s.threshold {
			// Normal noise gate for low-level sounds
			gainReduction = strength / s.threshold
			if gainReduction < s.gainFactor {
				gainReduction = s.gainFactor
			}
		}
		
		// Apply gain reduction
		processed := filtered * gainReduction
		
		// Convert back to int16 with proper clipping
		processedInt := processed * 32767.0
		if processedInt > 32767 {
			processedInt = 32767
		} else if processedInt < -32767 {
			processedInt = -32767
		}
		
		samples[i] = int16(processedInt)
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