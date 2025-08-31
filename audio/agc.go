package audio

import (
	"math"
)

// AGC (Automatic Gain Control) processor for voice normalization
type AGC struct {
	targetLevel    float32 // Target RMS level (0.0-1.0)
	maxGain        float32 // Maximum gain multiplier
	minGain        float32 // Minimum gain multiplier
	attackTime     float32 // Attack time coefficient
	releaseTime    float32 // Release time coefficient
	currentGain    float32 // Current gain value
	envelope       float32 // Signal envelope
	enabled        bool    // Whether AGC is enabled
	compThreshold  float32 // Compression threshold
	compRatio      float32 // Compression ratio
}

// NewAGC creates a new AGC processor with sensible defaults for voice
func NewAGC() *AGC {
	return &AGC{
		targetLevel:    0.15,  // Target 15% of max amplitude
		maxGain:        6.0,   // Maximum 6x gain (about 15.5dB)
		minGain:        0.1,   // Minimum 0.1x gain (-20dB)
		attackTime:     0.001, // Fast attack (1ms)
		releaseTime:    0.1,   // Slower release (100ms)
		currentGain:    1.0,   // Start with unity gain
		envelope:       0.0,   // Start with zero envelope
		enabled:        true,  // Enable by default
		compThreshold:  0.7,   // Compress signals above 70%
		compRatio:      3.0,   // 3:1 compression ratio
	}
}

// ProcessSamples applies AGC processing to audio samples
func (agc *AGC) ProcessSamples(samples []int16) {
	if !agc.enabled || len(samples) == 0 {
		return
	}

	// Convert samples to float32 for processing
	floatSamples := make([]float32, len(samples))
	for i, sample := range samples {
		floatSamples[i] = float32(sample) / 32768.0
	}

	// Calculate RMS level for gain control
	rmsSum := float32(0.0)
	for _, sample := range floatSamples {
		rmsSum += sample * sample
	}
	rms := float32(math.Sqrt(float64(rmsSum / float32(len(floatSamples)))))

	// Update envelope with peak detection for compression
	peak := float32(0.0)
	for _, sample := range floatSamples {
		absample := float32(math.Abs(float64(sample)))
		if absample > peak {
			peak = absample
		}
	}

	// Envelope following
	if peak > agc.envelope {
		agc.envelope += (peak - agc.envelope) * agc.attackTime
	} else {
		agc.envelope += (peak - agc.envelope) * agc.releaseTime
	}

	// Calculate desired gain based on RMS
	var desiredGain float32
	if rms > 0.001 { // Avoid division by zero for very quiet signals
		desiredGain = agc.targetLevel / rms
	} else {
		desiredGain = agc.maxGain // Boost very quiet signals
	}

	// Apply gain limits
	if desiredGain > agc.maxGain {
		desiredGain = agc.maxGain
	}
	if desiredGain < agc.minGain {
		desiredGain = agc.minGain
	}

	// Smooth gain changes
	if desiredGain > agc.currentGain {
		agc.currentGain += (desiredGain - agc.currentGain) * agc.attackTime * 0.1
	} else {
		agc.currentGain += (desiredGain - agc.currentGain) * agc.releaseTime
	}

	// Apply AGC gain to samples
	for i, sample := range floatSamples {
		processed := sample * agc.currentGain

		// Apply compression for loud signals
		if agc.envelope > agc.compThreshold {
			// Calculate compression amount
			overage := agc.envelope - agc.compThreshold
			compAmount := overage / agc.compRatio
			compGain := (agc.compThreshold + compAmount) / agc.envelope
			processed *= compGain
		}

		// Soft limiting to prevent clipping
		if processed > 0.95 {
			processed = 0.95 + (processed-0.95)*0.1
		} else if processed < -0.95 {
			processed = -0.95 + (processed+0.95)*0.1
		}

		// Convert back to int16
		intSample := int32(processed * 32767.0)
		if intSample > 32767 {
			intSample = 32767
		} else if intSample < -32767 {
			intSample = -32767
		}
		samples[i] = int16(intSample)
	}
}

// SetEnabled enables or disables AGC processing
func (agc *AGC) SetEnabled(enabled bool) {
	agc.enabled = enabled
}

// IsEnabled returns whether AGC is enabled
func (agc *AGC) IsEnabled() bool {
	return agc.enabled
}

// SetTargetLevel sets the target RMS level (0.0-1.0)
func (agc *AGC) SetTargetLevel(level float32) {
	if level > 0.0 && level < 1.0 {
		agc.targetLevel = level
	}
}

// SetMaxGain sets the maximum gain multiplier
func (agc *AGC) SetMaxGain(gain float32) {
	if gain > 1.0 && gain <= 10.0 {
		agc.maxGain = gain
	}
}

// GetCurrentGain returns the current gain being applied
func (agc *AGC) GetCurrentGain() float32 {
	return agc.currentGain
}

// Reset resets the AGC state
func (agc *AGC) Reset() {
	agc.currentGain = 1.0
	agc.envelope = 0.0
}