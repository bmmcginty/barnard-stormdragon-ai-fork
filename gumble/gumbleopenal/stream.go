package gumbleopenal

import (
	"encoding/binary"
	"errors"
	"os/exec"
	"sync"
	"time"

	"git.stormux.org/storm/barnard/audio"
	"git.stormux.org/storm/barnard/gumble/go-openal/openal"
	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/noise"
)

// NoiseProcessor interface for noise suppression
type NoiseProcessor interface {
	ProcessSamples(samples []int16)
	IsEnabled() bool
}

// EffectsProcessor interface for voice effects
type EffectsProcessor interface {
	ProcessSamples(samples []int16)
	IsEnabled() bool
}

// FilePlayer interface for file playback
type FilePlayer interface {
	GetAudioFrame() []int16
	IsPlaying() bool
}

type Recorder interface {
	RecordAudioFrame(source uint32, samples []int16)
}

const recorderOutgoingSource uint32 = ^uint32(0)

const (
	maxBufferSize = 11520 // Max frame size (2880) * bytes per stereo sample (4)
)

var (
	ErrState        = errors.New("gumbleopenal: invalid state")
	ErrMic          = errors.New("gumbleopenal: microphone disconnected or misconfigured")
	ErrInputDevice  = errors.New("gumbleopenal: invalid input device or parameters")
	ErrOutputDevice = errors.New("gumbleopenal: invalid output device or parameters")
)

func beep() {
	cmd := exec.Command("beep")
	cmdout, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	if cmdout != nil {
	}
}

type Stream struct {
	client *gumble.Client
	link   gumble.Detacher

	deviceSource    *openal.CaptureDevice
	sourceFormat    openal.Format
	sourceChannels  int
	sourceFrameSize int
	micVolume       float32
	sourceStop      chan bool

	deviceSink  *openal.Device
	contextSink *openal.Context

	noiseProcessor        NoiseProcessor
	noiseProcessorRight   NoiseProcessor
	micAGC                *audio.AGC
	micAGCRight           *audio.AGC
	effectsProcessor      EffectsProcessor
	effectsProcessorRight EffectsProcessor
	filePlayer            FilePlayer
	recorderMu            sync.RWMutex
	recorder              Recorder
}

func New(client *gumble.Client, inputDevice *string, outputDevice *string, test bool) (*Stream, error) {
	frmsz := 480
	if !test {
		frmsz = client.Config.AudioFrameSize()
	}

	inputFormat := openal.FormatStereo16
	sourceChannels := 2
	idev := openal.CaptureOpenDevice(*inputDevice, gumble.AudioSampleRate, inputFormat, uint32(frmsz))
	if idev == nil {
		inputFormat = openal.FormatMono16
		sourceChannels = 1
		idev = openal.CaptureOpenDevice(*inputDevice, gumble.AudioSampleRate, inputFormat, uint32(frmsz))
	}
	if idev == nil {
		return nil, ErrInputDevice
	}

	odev := openal.OpenDevice(*outputDevice)
	if odev == nil {
		idev.CaptureCloseDevice()
		return nil, ErrOutputDevice
	}

	if test {
		idev.CaptureCloseDevice()
		odev.CloseDevice()
		return nil, nil
	}

	s := &Stream{
		client:          client,
		sourceFormat:    inputFormat,
		sourceChannels:  sourceChannels,
		sourceFrameSize: frmsz,
		micVolume:       1.0,
		micAGC:          audio.NewAGC(), // Always enable AGC for outgoing mic
	}
	if sourceChannels == 2 {
		s.micAGCRight = audio.NewAGC()
	}

	s.deviceSource = idev
	if s.deviceSource == nil {
		return nil, ErrInputDevice
	}

	s.deviceSink = odev
	if s.deviceSink == nil {
		return nil, ErrOutputDevice
	}
	s.contextSink = s.deviceSink.CreateContext()
	if s.contextSink == nil {
		s.Destroy()
		return nil, ErrOutputDevice
	}
	s.contextSink.Activate()

	return s, nil
}

func (s *Stream) AttachStream(client *gumble.Client) {
	s.link = client.Config.AttachAudio(s)
}

func (s *Stream) SetNoiseProcessor(np NoiseProcessor) {
	s.noiseProcessor = np
	s.noiseProcessorRight = cloneNoiseProcessor(np)
}

func (s *Stream) SetEffectsProcessor(ep EffectsProcessor) {
	s.effectsProcessor = ep
	s.effectsProcessorRight = cloneEffectsProcessor(ep)
}

func (s *Stream) GetEffectsProcessor() EffectsProcessor {
	return s.effectsProcessor
}

func (s *Stream) SetFilePlayer(fp FilePlayer) {
	s.filePlayer = fp
}

func (s *Stream) GetFilePlayer() FilePlayer {
	return s.filePlayer
}

func (s *Stream) SetRecorder(recorder Recorder) {
	s.recorderMu.Lock()
	defer s.recorderMu.Unlock()
	s.recorder = recorder
}

func (s *Stream) getRecorder() Recorder {
	s.recorderMu.RLock()
	defer s.recorderMu.RUnlock()
	return s.recorder
}

func (s *Stream) Destroy() {
	if s.link != nil {
		s.link.Detach()
	}
	if s.deviceSource != nil {
		s.StopSource()
		s.deviceSource.CaptureCloseDevice()
		s.deviceSource = nil
	}
	if s.deviceSink != nil {
		s.contextSink.Destroy()
		s.deviceSink.CloseDevice()
		s.contextSink = nil
		s.deviceSink = nil
	}
}

func (s *Stream) StartSource(inputDevice *string) error {
	if s.sourceStop != nil {
		return ErrState
	}
	if s.deviceSource == nil {
		return ErrMic
	}
	s.deviceSource.CaptureStart()
	s.sourceStop = make(chan bool)
	go s.sourceRoutine(inputDevice)
	return nil
}

func (s *Stream) StopSource() error {
	if s.deviceSource == nil {
		return ErrMic
	}
	s.deviceSource.CaptureStop()
	if s.sourceStop == nil {
		return ErrState
	}
	close(s.sourceStop)
	s.sourceStop = nil
	return nil
}

func (s *Stream) GetMicVolume() float32 {
	return s.micVolume
}

func (s *Stream) SetMicVolume(change float32, relative bool) {
	var val float32
	if relative {
		val = s.GetMicVolume() + change
	} else {
		val = change
	}
	if val >= 1 {
		val = 1.0
	}
	if val <= 0 {
		val = 0
	}
	s.micVolume = val
}

func (s *Stream) OnAudioStream(e *gumble.AudioStreamEvent) {
	go func(e *gumble.AudioStreamEvent) {
		var source = openal.NewSource()
		e.User.AudioSource = &source

		// Set initial gain based on volume and mute state
		if e.User.LocallyMuted {
			e.User.AudioSource.SetGain(0)
		} else {
			e.User.AudioSource.SetGain(e.User.Volume)
		}

		bufferCount := e.Client.Config.Buffers
		if bufferCount < 64 {
			bufferCount = 64
		}
		emptyBufs := openal.NewBuffers(bufferCount)

		reclaim := func() {
			if n := source.BuffersProcessed(); n > 0 {
				reclaimedBufs := make(openal.Buffers, n)
				source.UnqueueBuffers(reclaimedBufs)
				emptyBufs = append(emptyBufs, reclaimedBufs...)
			}
		}

		var raw [maxBufferSize]byte

		for packet := range e.C {
			// Skip processing if user is locally muted
			if e.User.LocallyMuted {
				continue
			}

			var boost uint16 = uint16(1)
			samples := len(packet.AudioBuffer)
			if samples > cap(raw)/2 {
				continue
			}

			boost = e.User.Boost
			recorder := s.getRecorder()
			var recordBuffer []int16
			recordPtr := 0
			if recorder != nil {
				recordBuffer = make([]int16, len(packet.AudioBuffer)*gumble.AudioChannels)
			}

			// Check if sample count suggests stereo data
			isStereo := samples > gumble.AudioDefaultFrameSize && samples%2 == 0
			format := openal.FormatMono16
			if isStereo {
				format = openal.FormatStereo16
				samples = samples / 2
			}

			rawPtr := 0
			if isStereo {
				// Process stereo samples as pairs
				for i := 0; i < samples*2; i += 2 {
					// Process left channel with saturation protection
					sample := packet.AudioBuffer[i]
					if boost > 1 {
						boosted := int32(sample) * int32(boost)
						if boosted > 32767 {
							sample = 32767
						} else if boosted < -32767 {
							sample = -32767
						} else {
							sample = int16(boosted)
						}
					}
					if recorder != nil {
						recordBuffer[recordPtr] = scaleForRecording(sample, e.User.Volume)
						recordPtr++
					}
					binary.LittleEndian.PutUint16(raw[rawPtr:], uint16(sample))
					rawPtr += 2

					// Process right channel with saturation protection
					sample = packet.AudioBuffer[i+1]
					if boost > 1 {
						boosted := int32(sample) * int32(boost)
						if boosted > 32767 {
							sample = 32767
						} else if boosted < -32767 {
							sample = -32767
						} else {
							sample = int16(boosted)
						}
					}
					if recorder != nil {
						recordBuffer[recordPtr] = scaleForRecording(sample, e.User.Volume)
						recordPtr++
					}
					binary.LittleEndian.PutUint16(raw[rawPtr:], uint16(sample))
					rawPtr += 2
				}
			} else {
				// Process mono samples with saturation protection
				for i := 0; i < samples; i++ {
					sample := packet.AudioBuffer[i]
					if boost > 1 {
						boosted := int32(sample) * int32(boost)
						if boosted > 32767 {
							sample = 32767
						} else if boosted < -32767 {
							sample = -32767
						} else {
							sample = int16(boosted)
						}
					}
					if recorder != nil {
						recordSample := scaleForRecording(sample, e.User.Volume)
						recordBuffer[recordPtr] = recordSample
						recordBuffer[recordPtr+1] = recordSample
						recordPtr += 2
					}
					binary.LittleEndian.PutUint16(raw[rawPtr:], uint16(sample))
					rawPtr += 2
				}
			}
			if recorder != nil && recordPtr > 0 {
				recorder.RecordAudioFrame(e.User.Session, recordBuffer[:recordPtr])
			}

			reclaim()
			if len(emptyBufs) == 0 {
				continue
			}

			last := len(emptyBufs) - 1
			buffer := emptyBufs[last]
			emptyBufs = emptyBufs[:last]

			buffer.SetData(format, raw[:rawPtr], gumble.AudioSampleRate)
			source.QueueBuffer(buffer)

			if source.State() != openal.Playing {
				source.Play()
			}
		}
		reclaim()
		emptyBufs.Delete()
		source.Delete()
	}(e)
}

func (s *Stream) sourceRoutine(inputDevice *string) {
	interval := s.client.Config.AudioInterval
	frameSize := s.client.Config.AudioFrameSize()

	if frameSize != s.sourceFrameSize {
		s.deviceSource.CaptureCloseDevice()
		s.sourceFrameSize = frameSize
		s.deviceSource = openal.CaptureOpenDevice(*inputDevice, gumble.AudioSampleRate, s.sourceFormat, uint32(s.sourceFrameSize))
		if s.deviceSource == nil && s.sourceFormat == openal.FormatStereo16 {
			s.sourceFormat = openal.FormatMono16
			s.sourceChannels = 1
			s.deviceSource = openal.CaptureOpenDevice(*inputDevice, gumble.AudioSampleRate, s.sourceFormat, uint32(s.sourceFrameSize))
		}
	}
	if s.deviceSource == nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	stop := s.sourceStop

	outgoing := s.client.AudioOutgoing()
	defer close(outgoing)

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			sampleCount := frameSize * s.sourceChannels
			int16Buffer := make([]int16, sampleCount)

			// Capture microphone if available
			hasMicInput := false
			buff := s.deviceSource.CaptureSamples(uint32(frameSize))
			if len(buff) == sampleCount*2 {
				hasMicInput = true
				for i := 0; i < sampleCount; i++ {
					sample := int16(binary.LittleEndian.Uint16(buff[i*2:]))
					if s.micVolume != 1.0 {
						sample = int16(float32(sample) * s.micVolume)
					}
					int16Buffer[i] = sample
				}

				if s.sourceChannels == 1 {
					s.processMonoSamples(int16Buffer)
				} else {
					s.processStereoSamples(int16Buffer, frameSize)
				}
			}

			// Mix with or use file audio if playing
			hasFileAudio := false
			var outputBuffer []int16

			if s.filePlayer != nil && s.filePlayer.IsPlaying() {
				fileAudio := s.filePlayer.GetAudioFrame()
				if fileAudio != nil && len(fileAudio) > 0 {
					hasFileAudio = true
					// File audio is stereo - send as stereo when file is playing
					// Create stereo buffer (frameSize * 2 channels)
					outputBuffer = make([]int16, frameSize*2)

					if hasMicInput {
						if s.sourceChannels == 2 {
							// Mix stereo mic with stereo file
							for i := 0; i < frameSize; i++ {
								idx := i * 2
								if idx+1 < len(fileAudio) {
									left := int32(int16Buffer[idx]) + int32(fileAudio[idx])
									if left > 32767 {
										left = 32767
									} else if left < -32768 {
										left = -32768
									}
									outputBuffer[idx] = int16(left)

									right := int32(int16Buffer[idx+1]) + int32(fileAudio[idx+1])
									if right > 32767 {
										right = 32767
									} else if right < -32768 {
										right = -32768
									}
									outputBuffer[idx+1] = int16(right)
								}
							}
						} else {
							// Mix mono mic with stereo file
							for i := 0; i < frameSize; i++ {
								idx := i * 2
								if idx+1 < len(fileAudio) {
									left := int32(int16Buffer[i]) + int32(fileAudio[idx])
									if left > 32767 {
										left = 32767
									} else if left < -32768 {
										left = -32768
									}
									outputBuffer[idx] = int16(left)

									right := int32(int16Buffer[i]) + int32(fileAudio[idx+1])
									if right > 32767 {
										right = 32767
									} else if right < -32768 {
										right = -32768
									}
									outputBuffer[idx+1] = int16(right)
								}
							}
						}
					} else {
						// Use file audio only (already stereo)
						copy(outputBuffer, fileAudio[:frameSize*2])
					}
				}
			}

			// Determine what to send
			if hasFileAudio {
				// Send stereo buffer when file is playing
				outgoing <- gumble.AudioBuffer(outputBuffer)
				if recorder := s.getRecorder(); recorder != nil {
					recorder.RecordAudioFrame(recorderOutgoingSource, outputBuffer)
				}
			} else if hasMicInput {
				// Send mic when no file is playing
				outgoing <- gumble.AudioBuffer(int16Buffer)
				if recorder := s.getRecorder(); recorder != nil {
					recorder.RecordAudioFrame(recorderOutgoingSource, int16Buffer)
				}
			}
		}
	}
}

func scaleForRecording(sample int16, volume float32) int16 {
	scaled := int32(float32(sample) * volume)
	if scaled > 32767 {
		return 32767
	}
	if scaled < -32768 {
		return -32768
	}
	return int16(scaled)
}

func (s *Stream) processMonoSamples(samples []int16) {
	s.processChannel(samples, s.noiseProcessor, s.micAGC, s.effectsProcessor)
}

func (s *Stream) processStereoSamples(samples []int16, frameSize int) {
	if frameSize == 0 || len(samples) < frameSize*2 {
		return
	}

	s.ensureStereoProcessors()
	s.syncStereoProcessors()

	left := make([]int16, frameSize)
	right := make([]int16, frameSize)

	for i := 0; i < frameSize; i++ {
		idx := i * 2
		left[i] = samples[idx]
		right[i] = samples[idx+1]
	}

	s.processChannel(left, s.noiseProcessor, s.micAGC, s.effectsProcessor)
	s.processChannel(right, s.noiseProcessorRight, s.micAGCRight, s.effectsProcessorRight)

	for i := 0; i < frameSize; i++ {
		idx := i * 2
		samples[idx] = left[i]
		samples[idx+1] = right[i]
	}
}

func (s *Stream) processChannel(samples []int16, noiseProcessor NoiseProcessor, micAGC *audio.AGC, effectsProcessor EffectsProcessor) {
	if noiseProcessor != nil && noiseProcessor.IsEnabled() {
		noiseProcessor.ProcessSamples(samples)
	}
	if micAGC != nil {
		micAGC.ProcessSamples(samples)
	}
	if effectsProcessor != nil && effectsProcessor.IsEnabled() {
		effectsProcessor.ProcessSamples(samples)
	}
}

func (s *Stream) ensureStereoProcessors() {
	if s.micAGCRight == nil {
		s.micAGCRight = audio.NewAGC()
	}
	if s.noiseProcessorRight == nil {
		s.noiseProcessorRight = cloneNoiseProcessor(s.noiseProcessor)
	}
	if s.effectsProcessorRight == nil {
		s.effectsProcessorRight = cloneEffectsProcessor(s.effectsProcessor)
	}
}

func (s *Stream) syncStereoProcessors() {
	leftSuppressor, leftOk := s.noiseProcessor.(*noise.Suppressor)
	rightSuppressor, rightOk := s.noiseProcessorRight.(*noise.Suppressor)
	if leftOk && rightOk {
		if leftSuppressor.IsEnabled() != rightSuppressor.IsEnabled() {
			rightSuppressor.SetEnabled(leftSuppressor.IsEnabled())
		}
	}

	leftEffects, leftOk := s.effectsProcessor.(*audio.EffectsProcessor)
	rightEffects, rightOk := s.effectsProcessorRight.(*audio.EffectsProcessor)
	if leftOk && rightOk {
		if leftEffects.IsEnabled() != rightEffects.IsEnabled() {
			rightEffects.SetEnabled(leftEffects.IsEnabled())
		}
		if leftEffects.GetCurrentEffect() != rightEffects.GetCurrentEffect() {
			rightEffects.SetEffect(leftEffects.GetCurrentEffect())
		}
	}
}

func cloneNoiseProcessor(np NoiseProcessor) NoiseProcessor {
	if np == nil {
		return nil
	}
	if suppressor, ok := np.(*noise.Suppressor); ok {
		clone := noise.NewSuppressor()
		clone.SetEnabled(suppressor.IsEnabled())
		return clone
	}
	return nil
}

func cloneEffectsProcessor(ep EffectsProcessor) EffectsProcessor {
	if ep == nil {
		return nil
	}
	if processor, ok := ep.(*audio.EffectsProcessor); ok {
		clone := audio.NewEffectsProcessor(gumble.AudioSampleRate)
		clone.SetEnabled(processor.IsEnabled())
		clone.SetEffect(processor.GetCurrentEffect())
		return clone
	}
	return nil
}
