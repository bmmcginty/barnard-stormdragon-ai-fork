package noise

/*
#cgo pkg-config: rnnoise
#include <stdlib.h>
#include <rnnoise.h>
*/
import "C"

import (
	"math"
	"runtime"
	"sync"
)

// Ensure Suppressor implements the NoiseProcessor interface.
var _ interface {
	ProcessSamples(samples []int16)
	IsEnabled() bool
} = (*Suppressor)(nil)

// Suppressor handles RNNoise-backed noise suppression for microphone samples.
type Suppressor struct {
	mu        sync.Mutex
	state     *C.DenoiseState
	enabled   bool
	frameSize int
}

// NewSuppressor creates a new RNNoise suppressor.
func NewSuppressor() *Suppressor {
	state := C.rnnoise_create(nil)
	if state == nil {
		panic("rnnoise: failed to create denoise state")
	}

	s := &Suppressor{
		state:     state,
		frameSize: int(C.rnnoise_get_frame_size()),
	}
	if s.frameSize <= 0 {
		C.rnnoise_destroy(state)
		panic("rnnoise: invalid frame size")
	}
	runtime.SetFinalizer(s, (*Suppressor).Close)
	return s
}

// Close releases the RNNoise state.
func (s *Suppressor) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == nil {
		return
	}
	C.rnnoise_destroy(s.state)
	s.state = nil
}

// SetEnabled enables or disables noise suppression.
func (s *Suppressor) SetEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = enabled
}

// IsEnabled returns whether noise suppression is enabled.
func (s *Suppressor) IsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// ProcessSamples applies RNNoise suppression to complete frames in samples.
func (s *Suppressor) ProcessSamples(samples []int16) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled || s.state == nil || len(samples) < s.frameSize {
		return
	}

	input := make([]C.float, s.frameSize)
	output := make([]C.float, s.frameSize)
	for offset := 0; offset+s.frameSize <= len(samples); offset += s.frameSize {
		frame := samples[offset : offset+s.frameSize]
		for i, sample := range frame {
			input[i] = C.float(sample)
		}
		C.rnnoise_process_frame(s.state, &output[0], &input[0])
		for i, sample := range output {
			frame[i] = floatToInt16(float32(sample))
		}
	}
}

func floatToInt16(sample float32) int16 {
	if sample > math.MaxInt16 {
		return math.MaxInt16
	}
	if sample < math.MinInt16 {
		return math.MinInt16
	}
	return int16(sample)
}
