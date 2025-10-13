package audio

import (
	"math"
)

// VoiceEffect represents different voice effect types
type VoiceEffect int

const (
	EffectNone VoiceEffect = iota
	EffectEcho
	EffectReverb
	EffectHighPitch
	EffectLowPitch
	EffectRobot
	EffectChorus
	EffectCount // Keep this last for cycling
)

// String returns the name of the effect
func (e VoiceEffect) String() string {
	switch e {
	case EffectNone:
		return "None"
	case EffectEcho:
		return "Echo"
	case EffectReverb:
		return "Reverb"
	case EffectHighPitch:
		return "High Pitch"
	case EffectLowPitch:
		return "Low Pitch"
	case EffectRobot:
		return "Robot"
	case EffectChorus:
		return "Chorus"
	default:
		return "Unknown"
	}
}

// EffectsProcessor handles voice effects processing
type EffectsProcessor struct {
	currentEffect VoiceEffect
	enabled       bool

	// Echo parameters
	echoDelay     int     // Delay in samples
	echoFeedback  float32 // Echo feedback amount (0-1)
	echoMix       float32 // Mix of echo with original (0-1)
	echoBuffer    []int16 // Circular buffer for echo
	echoPosition  int     // Current position in echo buffer

	// Reverb buffer
	reverseInputBuffer []int16 // Delay line for reverb
	reverseInputPos    int     // Write position in buffer

	// Pitch shift parameters
	pitchRatio    float32 // Pitch shift ratio
	pitchBuffer   []int16 // Buffer for pitch shifting
	pitchPhase    float32 // Phase accumulator for resampling

	// Robot voice parameters
	robotFreq     float32 // Modulation frequency
	robotPhase    float32 // Phase accumulator
	sampleRate    float32 // Audio sample rate

	// Chorus parameters
	chorusDelays  []int   // Multiple delay times
	chorusBuffers [][]int16 // Multiple delay buffers
	chorusPositions []int  // Positions in chorus buffers
	chorusRates   []float32 // LFO rates for each chorus voice
	chorusPhases  []float32 // LFO phases
}

// NewEffectsProcessor creates a new voice effects processor
func NewEffectsProcessor(sampleRate int) *EffectsProcessor {
	echoDelay := sampleRate / 4 // 250ms delay

	return &EffectsProcessor{
		currentEffect: EffectNone,
		enabled:       true,
		sampleRate:    float32(sampleRate),

		// Echo setup
		echoDelay:    echoDelay,
		echoFeedback: 0.4,
		echoMix:      0.5,
		echoBuffer:   make([]int16, echoDelay),
		echoPosition: 0,

		// Reverb setup - 100ms buffer for delay lines
		reverseInputBuffer: make([]int16, sampleRate/10), // 100ms buffer
		reverseInputPos:    0,

		// Pitch shift setup
		pitchRatio:  1.0,
		pitchBuffer: make([]int16, 4096),
		pitchPhase:  0.0,

		// Robot voice setup
		robotFreq:  30.0, // 30 Hz modulation
		robotPhase: 0.0,

		// Chorus setup (3 voices)
		chorusDelays:    []int{sampleRate/50, sampleRate/40, sampleRate/35}, // ~20-30ms
		chorusBuffers:   make([][]int16, 3),
		chorusPositions: make([]int, 3),
		chorusRates:     []float32{1.5, 2.0, 2.3}, // LFO rates in Hz
		chorusPhases:    make([]float32, 3),
	}
}

// GetCurrentEffect returns the current effect
func (ep *EffectsProcessor) GetCurrentEffect() VoiceEffect {
	return ep.currentEffect
}

// SetEffect sets the current effect
func (ep *EffectsProcessor) SetEffect(effect VoiceEffect) {
	if effect >= 0 && effect < EffectCount {
		ep.currentEffect = effect
		ep.resetBuffers()
	}
}

// CycleEffect cycles to the next effect
func (ep *EffectsProcessor) CycleEffect() VoiceEffect {
	ep.currentEffect = (ep.currentEffect + 1) % EffectCount
	ep.resetBuffers()
	return ep.currentEffect
}

// SetEnabled enables or disables effects processing
func (ep *EffectsProcessor) SetEnabled(enabled bool) {
	ep.enabled = enabled
}

// IsEnabled returns whether effects are enabled
func (ep *EffectsProcessor) IsEnabled() bool {
	return ep.enabled
}

// ProcessSamples applies the current voice effect to audio samples
func (ep *EffectsProcessor) ProcessSamples(samples []int16) {
	if !ep.enabled || ep.currentEffect == EffectNone || len(samples) == 0 {
		return
	}

	switch ep.currentEffect {
	case EffectEcho:
		ep.processEcho(samples)
	case EffectReverb:
		ep.processReverb(samples)
	case EffectHighPitch:
		ep.processPitchShift(samples, 1.5)
	case EffectLowPitch:
		ep.processPitchShift(samples, 0.75)
	case EffectRobot:
		ep.processRobot(samples)
	case EffectChorus:
		ep.processChorus(samples)
	}
}

// processEcho applies echo effect
func (ep *EffectsProcessor) processEcho(samples []int16) {
	for i := range samples {
		// Get delayed sample
		delayedSample := ep.echoBuffer[ep.echoPosition]

		// Mix original with echo
		outputSample := float32(samples[i])*(1.0-ep.echoMix) +
		               float32(delayedSample)*ep.echoMix

		// Create new echo sample (current + feedback)
		newEchoSample := float32(samples[i]) + float32(delayedSample)*ep.echoFeedback

		// Store in buffer with clipping
		if newEchoSample > 32767 {
			newEchoSample = 32767
		} else if newEchoSample < -32767 {
			newEchoSample = -32767
		}
		ep.echoBuffer[ep.echoPosition] = int16(newEchoSample)

		// Advance buffer position
		ep.echoPosition = (ep.echoPosition + 1) % len(ep.echoBuffer)

		// Apply to output with clipping
		if outputSample > 32767 {
			outputSample = 32767
		} else if outputSample < -32767 {
			outputSample = -32767
		}
		samples[i] = int16(outputSample)
	}
}

// processReverb applies reverb effect - like echo but with multiple short delays
func (ep *EffectsProcessor) processReverb(samples []int16) {
	bufLen := len(ep.reverseInputBuffer)

	// Three quick echoes instead of one long repeating echo
	delays := []int{
		bufLen / 8,  // ~12.5ms
		bufLen / 5,  // ~20ms
		bufLen / 3,  // ~33ms
	}
	gains := []float32{0.3, 0.2, 0.15}

	for i := range samples {
		// Store current sample
		ep.reverseInputBuffer[ep.reverseInputPos] = samples[i]

		// Add the three quick echoes
		reverbSample := float32(0)
		for j := 0; j < len(delays); j++ {
			readPos := (ep.reverseInputPos - delays[j] + bufLen) % bufLen
			reverbSample += float32(ep.reverseInputBuffer[readPos]) * gains[j]
		}

		// Mix dry and wet signal
		outputSample := float32(samples[i])*0.7 + reverbSample

		// Advance position
		ep.reverseInputPos = (ep.reverseInputPos + 1) % bufLen

		// Apply with clipping
		if outputSample > 32767 {
			outputSample = 32767
		} else if outputSample < -32767 {
			outputSample = -32767
		}

		samples[i] = int16(outputSample)
	}
}

// processPitchShift applies pitch shifting using cubic interpolation
func (ep *EffectsProcessor) processPitchShift(samples []int16, ratio float32) {
	if ratio == 1.0 {
		return
	}

	bufLen := len(ep.pitchBuffer)

	// Copy samples to pitch buffer (maintaining history)
	copy(ep.pitchBuffer[bufLen-len(samples):], samples)

	// Resample using cubic interpolation for smoother output
	for i := range samples {
		// Calculate source position
		srcPos := float32(bufLen-len(samples)) + float32(i)*ratio

		// Bounds check with extra padding for cubic interpolation
		if srcPos >= float32(bufLen-2) {
			srcPos = float32(bufLen - 3)
		}
		if srcPos < 1 {
			srcPos = 1
		}

		// Cubic interpolation (Hermite interpolation)
		idx := int(srcPos)
		frac := srcPos - float32(idx)

		// Get 4 samples around the target position
		y0 := float32(ep.pitchBuffer[idx-1])
		y1 := float32(ep.pitchBuffer[idx])
		y2 := float32(ep.pitchBuffer[idx+1])
		y3 := float32(ep.pitchBuffer[idx+2])

		// Cubic Hermite interpolation
		c0 := y1
		c1 := 0.5 * (y2 - y0)
		c2 := y0 - 2.5*y1 + 2.0*y2 - 0.5*y3
		c3 := 0.5*(y3-y0) + 1.5*(y1-y2)

		interpolated := c0 + c1*frac + c2*frac*frac + c3*frac*frac*frac

		// Soft clipping to reduce harshness
		if interpolated > 32767 {
			interpolated = 32767
		} else if interpolated < -32767 {
			interpolated = -32767
		}

		samples[i] = int16(interpolated)
	}

	// Shift buffer for next frame
	copy(ep.pitchBuffer, ep.pitchBuffer[len(samples):])
}

// processRobot applies ring modulation for robot voice
func (ep *EffectsProcessor) processRobot(samples []int16) {
	phaseIncrement := 2.0 * math.Pi * ep.robotFreq / ep.sampleRate

	for i := range samples {
		// Generate carrier wave (sine wave)
		carrier := float32(math.Sin(float64(ep.robotPhase)))

		// Ring modulation: multiply signal by carrier
		modulated := float32(samples[i]) * (0.5 + carrier*0.5)

		// Advance phase
		ep.robotPhase += phaseIncrement
		if ep.robotPhase >= 2.0*math.Pi {
			ep.robotPhase -= 2.0 * math.Pi
		}

		// Apply with clipping
		if modulated > 32767 {
			modulated = 32767
		} else if modulated < -32767 {
			modulated = -32767
		}

		samples[i] = int16(modulated)
	}
}

// processChorus applies chorus effect with multiple delayed voices
func (ep *EffectsProcessor) processChorus(samples []int16) {
	// Initialize chorus buffers if needed
	for j := range ep.chorusBuffers {
		if len(ep.chorusBuffers[j]) == 0 {
			ep.chorusBuffers[j] = make([]int16, ep.chorusDelays[j])
		}
	}

	for i := range samples {
		output := float32(samples[i]) * 0.4 // Original signal at 40%

		// Add multiple chorus voices
		for j := 0; j < len(ep.chorusDelays); j++ {
			// LFO modulation for slight pitch variation
			lfoPhaseInc := 2.0 * math.Pi * ep.chorusRates[j] / ep.sampleRate
			lfo := float32(math.Sin(float64(ep.chorusPhases[j])))
			ep.chorusPhases[j] += lfoPhaseInc
			if ep.chorusPhases[j] >= 2.0*math.Pi {
				ep.chorusPhases[j] -= 2.0 * math.Pi
			}

			// Get delayed sample with LFO modulation
			modDelay := int(float32(ep.chorusDelays[j]) * (1.0 + lfo*0.03))
			if modDelay >= len(ep.chorusBuffers[j]) {
				modDelay = len(ep.chorusBuffers[j]) - 1
			}

			readPos := (ep.chorusPositions[j] - modDelay + len(ep.chorusBuffers[j])) % len(ep.chorusBuffers[j])
			delayedSample := ep.chorusBuffers[j][readPos]

			// Add this voice to output (20% each)
			output += float32(delayedSample) * 0.2

			// Store current sample in buffer
			ep.chorusBuffers[j][ep.chorusPositions[j]] = samples[i]
			ep.chorusPositions[j] = (ep.chorusPositions[j] + 1) % len(ep.chorusBuffers[j])
		}

		// Apply with clipping
		if output > 32767 {
			output = 32767
		} else if output < -32767 {
			output = -32767
		}

		samples[i] = int16(output)
	}
}

// resetBuffers clears all effect buffers
func (ep *EffectsProcessor) resetBuffers() {
	// Clear echo buffer
	for i := range ep.echoBuffer {
		ep.echoBuffer[i] = 0
	}
	ep.echoPosition = 0

	// Clear reverb buffer
	for i := range ep.reverseInputBuffer {
		ep.reverseInputBuffer[i] = 0
	}
	ep.reverseInputPos = 0

	// Clear pitch buffer
	for i := range ep.pitchBuffer {
		ep.pitchBuffer[i] = 0
	}
	ep.pitchPhase = 0

	// Reset robot phase
	ep.robotPhase = 0

	// Clear chorus buffers
	for j := range ep.chorusBuffers {
		if len(ep.chorusBuffers[j]) > 0 {
			for i := range ep.chorusBuffers[j] {
				ep.chorusBuffers[j][i] = 0
			}
		}
		ep.chorusPositions[j] = 0
		ep.chorusPhases[j] = 0
	}
}
