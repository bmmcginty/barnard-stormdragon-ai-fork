package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble"
)

// ---------------------------------------------------------------------------
// 440 Hz tone generator
// ---------------------------------------------------------------------------

// StartToneGenerator begins generating a 440 Hz sine wave and writing it to
// the client's outgoing audio channel at the configured interval. It blocks
// until the stop channel is closed.
func StartToneGenerator(client *gumble.Client, stop <-chan struct{}) {
	interval := client.Config.AudioInterval
	frameSize := client.Config.AudioFrameSize() // mono samples per frame
	sampleRate := float64(gumble.AudioSampleRate)
	frequency := 440.0

	// Pre-compute one full sine wave cycle so we can just index into it.
	// This avoids calling math.Sin in the hot loop.
	phase := 0.0
	phaseIncrement := 2.0 * math.Pi * frequency / sampleRate

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	outgoing := client.AudioOutgoing()
	defer close(outgoing)

	fmt.Fprintf(os.Stderr, "tonetest: starting 440 Hz tone generator (frameSize=%d, interval=%v)\n",
		frameSize, interval)

	for {
		select {
		case <-stop:
			fmt.Fprintf(os.Stderr, "tonetest: tone generator stopped\n")
			return
		case <-ticker.C:
			buf := make([]int16, frameSize)
			for i := 0; i < frameSize; i++ {
				// Generate sine wave with amplitude 0.5 to avoid clipping
				buf[i] = int16(math.Sin(phase) * 16000) // ~ -6dBFS
				phase += phaseIncrement
				if phase > 2.0*math.Pi {
					phase -= 2.0 * math.Pi
				}
			}
			outgoing <- gumble.AudioBuffer(buf)
		}
	}
}

// ---------------------------------------------------------------------------
// Incoming audio file saver
// ---------------------------------------------------------------------------

// AudioFileSaver implements gumble.AudioListener and writes all incoming PCM
// audio to a single raw 16-bit little-endian stereo 48kHz file.
type AudioFileSaver struct {
	file    *os.File
	stop    chan struct{}
	mu      sync.Mutex
	stopped bool
	wg      sync.WaitGroup
}

// NewAudioFileSaver creates the output file and returns a configured saver.
// The file is raw PCM: s16le, stereo, 48000 Hz.
// Play it back with:
//
//	ffplay -f s16le -ar 48000 -ac 2 <file>
//
// or convert with:
//
//	ffmpeg -f s16le -ar 48000 -ac 2 -i <file> output.wav
func NewAudioFileSaver(path string) (*AudioFileSaver, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "tonetest: saving incoming audio to %s\n", path)
	return &AudioFileSaver{
		file: f,
		stop: make(chan struct{}),
	}, nil
}

// Stop closes the stop channel, waits for all stream goroutines to finish,
// and closes the output file.
func (s *AudioFileSaver) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stop)
	s.mu.Unlock()

	s.wg.Wait()
	s.mu.Lock()
	_ = s.file.Close()
	s.mu.Unlock()
}

// OnAudioStream implements gumble.AudioListener.
func (s *AudioFileSaver) OnAudioStream(e *gumble.AudioStreamEvent) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()

	fmt.Fprintf(os.Stderr, "tonetest: incoming audio stream from %s\n", e.User.Name)
	go func() {
		defer s.wg.Done()

		for {
			select {
			case <-s.stop:
				return
			case packet, ok := <-e.C:
				if !ok {
					fmt.Fprintf(os.Stderr, "tonetest: audio stream from %s ended\n", e.User.Name)
					return
				}
				samples := packet.AudioBuffer
				if len(samples) == 0 {
					continue
				}

				// Write as raw PCM s16le.  The decoder outputs stereo
				// interleaved, so the sample count already accounts for
				// both channels.
				buf := make([]byte, len(samples)*2)
				for i, s := range samples {
					binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
				}
				s.mu.Lock()
				if _, err := s.file.Write(buf); err != nil {
					s.mu.Unlock()
					fmt.Fprintf(os.Stderr, "tonetest: write error: %v\n", err)
					return
				}
				s.mu.Unlock()
			}
		}
	}()
}
