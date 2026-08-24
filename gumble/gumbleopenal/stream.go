package gumbleopenal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"git.stormux.org/storm/barnard/audio"
	"git.stormux.org/storm/barnard/gumble/go-openal/openal"
	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/log"
	"git.stormux.org/storm/barnard/noise"
)

func deviceName(name string) string {
	if name == "" {
		return "default"
	}
	return name
}

func openInputDeviceError(name string, format openal.Format) error {
	return fmt.Errorf("%w: could not open capture device %q (format=%v, rate=%d)", ErrInputDevice, deviceName(name), format, gumble.AudioSampleRate)
}

func openOutputDeviceError(name string) error {
	return fmt.Errorf("%w: could not open playback device %q", ErrOutputDevice, deviceName(name))
}

// NoiseProcessor interface for noise suppression
type NoiseProcessor interface {
	ProcessSamples(samples []int16)
	IsEnabled() bool
}

// FilePlayer interface for file playback
type FilePlayer interface {
	GetAudioFrame() []int16
	IsPlaying() bool
}

type Recorder interface {
	RecordAudioFrame(source uint32, samples []int16, stereo bool)
}

const recorderOutgoingSource uint32 = ^uint32(0)

const (
	maxBufferSize    = 11520 // Max frame size (2880) * bytes per stereo sample (4)
	jitterMaxPackets = 50
	// Mumble destroys and recreates AudioInput when the sender switches audio
	// devices, which restarts its frame numbering at zero. The destructor
	// sends no terminator, so a sender that never unkeys leaves us expecting a
	// frame number the new stream will not reach for hours: every packet looks
	// permanently late and gets discarded. Detect that and resync.
	//
	// Two conditions must hold together. A sustained run of late packets
	// distinguishes a restarted stream from a clump of reordered packets,
	// which is bounded and then recovers on its own. The backwards jump must
	// also be too large to be network reordering; a smaller jump needs no
	// intervention because the restarted stream climbs back past the stale
	// expectation within jitterResyncJump frames anyway.
	jitterLateResync = 5
	// Frame numbers are Mumble timestamps in 10 ms units, so this is 1 second
	// — far beyond any real reordering window.
	jitterResyncJump = 100
)

// jitterShouldResync reports whether the sender restarted its frame numbering
// rather than merely delivering a few packets out of order. lateRun is the
// number of consecutive late packets and backJump is how far the current
// packet sits below the expected sequence.
func jitterShouldResync(lateRun int, backJump int64) bool {
	return lateRun >= jitterLateResync && backJump >= jitterResyncJump
}

// jitterPlaybackReady holds the requested initial playout delay only once.
// Requiring the delay on every packet drains and refills the renderer in bursts.
func jitterPlaybackReady(started bool, buffered, target time.Duration) bool {
	return started || buffered >= target
}

func audioPacketDuration(packet *gumble.AudioPacket) time.Duration {
	if packet == nil || len(packet.AudioBuffer) == 0 {
		return 0
	}
	// Opus decoders deliver interleaved stereo PCM to this renderer.
	frames := len(packet.AudioBuffer) / gumble.AudioChannels
	return time.Duration(frames) * time.Second / gumble.AudioSampleRate
}

var (
	ErrState        = errors.New("gumbleopenal: invalid state")
	ErrMic          = errors.New("gumbleopenal: microphone disconnected or misconfigured")
	ErrInputDevice  = errors.New("gumbleopenal: invalid input device or parameters")
	ErrOutputDevice = errors.New("gumbleopenal: invalid output device or parameters")
)

type renderCommand struct {
	fn   func()
	done chan struct{}
}

type Stream struct {
	client *gumble.Client
	link   gumble.Detacher

	deviceSource     *openal.CaptureDevice
	inputDeviceName  string
	outputDeviceName string
	sourceFormat     openal.Format
	sourceChannels   int
	sourceFrameSize  int
	micVolume        atomic.Uint32 // float32 stored as bits
	sourceMu         sync.Mutex
	sourceStop       chan bool
	sourceDone       chan struct{}

	deviceSink   *openal.Device
	contextSink  *openal.Context
	renderMu     sync.RWMutex
	renderCh     chan renderCommand
	renderDone   chan struct{}
	renderClosed bool

	noiseProcessor      NoiseProcessor
	noiseProcessorRight NoiseProcessor
	micAGC              *audio.AGC
	micAGCRight         *audio.AGC
	filePlayer          FilePlayer
	localSource         *openal.Source
	localBuffers        openal.Buffers
	recorderMu          sync.RWMutex
	errorFunc           func(error) // called on capture errors
	recorder            Recorder
	// streamWG tracks the per-user goroutines started by OnAudioStream. They
	// release their OpenAL source and buffers through the renderer, so Destroy
	// must let them finish before it tears the renderer down.
	streamWG sync.WaitGroup
}

func New(client *gumble.Client, inputDevice *string, outputDevice *string, test bool) (*Stream, error) {
	frmsz := 480
	if !test {
		frmsz = client.Config.AudioFrameSize()
	}

	devName := ""
	if inputDevice != nil {
		devName = *inputDevice
	}
	log.Info("OpenAL capture: requested device=%q rate=%d frameSize=%d", devName, gumble.AudioSampleRate, frmsz)

	inputFormat := openal.FormatStereo16
	sourceChannels := 2
	// Keep several frames in the capture ring so normal scheduler jitter does
	// not overflow a PipeWire/Pulse capture stream.
	captureBufferSize := uint32(frmsz * 4)
	idev := openal.CaptureOpenDevice(devName, gumble.AudioSampleRate, inputFormat, captureBufferSize)
	if idev == nil {
		log.Info("OpenAL capture: stereo failed, trying mono")
		inputFormat = openal.FormatMono16
		sourceChannels = 1
		idev = openal.CaptureOpenDevice(devName, gumble.AudioSampleRate, inputFormat, captureBufferSize)
	}
	if idev == nil {
		log.Error("OpenAL capture: failed to open device %q", devName)
		return nil, openInputDeviceError(devName, inputFormat)
	}
	if err := idev.Err(); err != nil {
		idev.CaptureCloseDevice()
		return nil, fmt.Errorf("%w: capture device %q: %v", ErrInputDevice, deviceName(devName), err)
	}
	log.Info("OpenAL capture: opened device %q format=%v channels=%d", devName, inputFormat, sourceChannels)

	outName := ""
	if outputDevice != nil {
		outName = *outputDevice
	}
	odev := openal.OpenDevice(outName)
	if odev == nil {
		idev.CaptureCloseDevice()
		return nil, openOutputDeviceError(outName)
	}
	if err := odev.Err(); err != nil {
		idev.CaptureCloseDevice()
		odev.CloseDevice()
		return nil, fmt.Errorf("%w: playback device %q: %v", ErrOutputDevice, deviceName(outName), err)
	}

	if test {
		idev.CaptureCloseDevice()
		odev.CloseDevice()
		return nil, nil
	}

	s := &Stream{
		client:           client,
		inputDeviceName:  devName,
		outputDeviceName: outName,
		sourceFormat:     inputFormat,
		sourceChannels:   sourceChannels,
		sourceFrameSize:  frmsz,
		micAGC:           audio.NewAGC(), // Always enable AGC for outgoing mic
	}
	s.micVolume.Store(math.Float32bits(1.0))
	if sourceChannels == 2 {
		s.micAGCRight = audio.NewAGC()
	}

	s.deviceSource = idev
	if s.deviceSource == nil {
		return nil, fmt.Errorf("%w: capture device %q is unavailable", ErrInputDevice, deviceName(devName))
	}

	s.deviceSink = odev
	if s.deviceSink == nil {
		return nil, fmt.Errorf("%w: playback device %q is unavailable", ErrOutputDevice, deviceName(outName))
	}
	s.contextSink = s.deviceSink.CreateContext()
	if s.contextSink == nil {
		err := s.deviceSink.Err()
		s.Destroy()
		if err != nil {
			return nil, fmt.Errorf("%w: creating context for playback device %q: %v", ErrOutputDevice, deviceName(outName), err)
		}
		return nil, fmt.Errorf("%w: could not create context for playback device %q", ErrOutputDevice, deviceName(outName))
	}
	// OpenAL contexts are current to an OS thread. Move ownership to one
	// dedicated render thread before any source or buffer is created.
	openal.NullContext.Activate()
	s.startRenderer()

	// Log OpenAL device info on the render thread
	s.render(func() {
		log.Info("OpenAL playback: vendor=%q version=%q renderer=%q",
			openal.GetString(0xB001),
			openal.GetString(0xB002),
			openal.GetString(0xB003))
	})

	return s, nil
}

func (s *Stream) startRenderer() {
	s.renderMu.Lock()
	s.renderClosed = false
	s.renderCh = make(chan renderCommand)
	s.renderDone = make(chan struct{})
	s.renderMu.Unlock()
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		s.contextSink.Activate()
		close(ready)
		defer close(s.renderDone)
		for command := range s.renderCh {
			command.fn()
			close(command.done)
		}
		openal.NullContext.Activate()
	}()
	<-ready
}

// render executes fn on the sole OS thread that owns the OpenAL context. It
// returns false after shutdown instead of sending to a closed renderer channel.
func (s *Stream) render(fn func()) bool {
	s.renderMu.RLock()
	defer s.renderMu.RUnlock()
	if s.renderClosed || s.renderCh == nil {
		return false
	}
	command := renderCommand{fn: fn, done: make(chan struct{})}
	s.renderCh <- command
	<-command.done
	return true
}

func (s *Stream) AttachStream(client *gumble.Client) {
	s.link = client.Config.AttachAudio(s)
}

func (s *Stream) SetNoiseProcessor(np NoiseProcessor) {
	s.noiseProcessor = np
	s.noiseProcessorRight = cloneNoiseProcessor(np)
}

// SetAGCEnabled turns microphone automatic gain control on or off. The AGC
// objects themselves are created up front, so this only flips their flag and is
// safe to call while capture is running.
func (s *Stream) SetAGCEnabled(enabled bool) {
	if s.micAGC != nil {
		s.micAGC.SetEnabled(enabled)
	}
	if s.micAGCRight != nil {
		s.micAGCRight.SetEnabled(enabled)
	}
}

// IsAGCEnabled reports whether microphone automatic gain control is active.
func (s *Stream) IsAGCEnabled() bool {
	return s.micAGC != nil && s.micAGC.IsEnabled()
}

func (s *Stream) SetFilePlayer(fp FilePlayer) {
	s.filePlayer = fp
	if player, ok := fp.(interface{ SetLocalPlayback(func([]byte)) }); ok {
		player.SetLocalPlayback(s.playLocalAudio)
	}
}

func (s *Stream) playLocalAudio(data []byte) {
	s.render(func() {
		if data == nil {
			if s.localSource != nil {
				s.localSource.Stop()
				queued := s.localSource.BuffersQueued()
				if queued > 0 {
					buffers := make(openal.Buffers, queued)
					s.localSource.UnqueueBuffers(buffers)
					s.localBuffers = append(s.localBuffers, buffers...)
				}
				s.localSource.Delete()
				s.localSource = nil
			}
			if len(s.localBuffers) > 0 {
				s.localBuffers.Delete()
				s.localBuffers = nil
			}
			return
		}
		if s.localSource == nil {
			source := openal.NewSource()
			source.SetGain(1)
			s.localSource = &source
			s.localBuffers = openal.NewBuffers(64)
		}
		if n := s.localSource.BuffersProcessed(); n > 0 {
			buffers := make(openal.Buffers, n)
			s.localSource.UnqueueBuffers(buffers)
			s.localBuffers = append(s.localBuffers, buffers...)
		}
		if len(s.localBuffers) == 0 {
			return
		}
		last := len(s.localBuffers) - 1
		buffer := s.localBuffers[last]
		s.localBuffers = s.localBuffers[:last]
		buffer.SetData(openal.FormatStereo16, data, gumble.AudioSampleRate)
		s.localSource.QueueBuffer(buffer)
		if s.localSource.State() != openal.Playing {
			s.localSource.Play()
		}
	})
}

func (s *Stream) GetFilePlayer() FilePlayer {
	return s.filePlayer
}

// UpdateUserGain applies a user's current mute and volume state on the
// renderer thread.
func (s *Stream) UpdateUserGain(user *gumble.User) {
	s.render(func() {
		if source := user.AudioSource(); source != nil {
			if user.LocallyMuted() {
				source.SetGain(0)
			} else {
				source.SetGain(user.Volume())
			}
		}
	})
}

// SetErrorFunc sets a callback that is invoked when the microphone
// capture device fails to provide audio data.
func (s *Stream) SetErrorFunc(f func(error)) {
	s.errorFunc = f
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

// destroyDrainTimeout bounds how long Destroy waits for the per-user audio
// goroutines to finish draining, so a wedged renderer cannot hang a reconnect.
const destroyDrainTimeout = 2 * time.Second

func (s *Stream) Destroy() {
	if s.link != nil {
		// Detach closes every per-user stream channel, which ends the
		// goroutines started by OnAudioStream.
		s.link.Detach()
	}
	// Those goroutines delete their OpenAL source and buffers through
	// s.render, which stops working the moment the renderer is closed below.
	// Waiting for them here is what keeps the device's sources and buffers
	// from being orphaned on every reconnect.
	drained := make(chan struct{})
	go func() {
		s.streamWG.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(destroyDrainTimeout):
		log.Warn("Destroy: timed out waiting for audio stream goroutines; " +
			"OpenAL sources and buffers may be released only by CloseDevice")
	}
	if s.deviceSource != nil {
		s.StopSource()
		if !s.deviceSource.CaptureCloseDevice() {
			log.Error("Destroy: closing capture device %q failed",
				deviceName(s.inputDeviceName))
		}
		s.deviceSource = nil
	}
	if s.deviceSink != nil {
		if s.contextSink != nil {
			s.render(func() {
				openal.NullContext.Activate()
				s.contextSink.Destroy()
			})
			s.renderMu.Lock()
			if !s.renderClosed {
				close(s.renderCh)
				s.renderClosed = true
			}
			s.renderMu.Unlock()
			<-s.renderDone
			s.contextSink = nil
		}
		// alcCloseDevice returns ALC_FALSE and frees nothing while the device
		// still has contexts, buffers or sources outstanding. Dropping the
		// handle after that silently leaks the device and every buffer it
		// owns, which is invisible to the Go GC, so at least report it.
		if !s.deviceSink.CloseDevice() {
			log.Error("Destroy: closing playback device %q failed; its OpenAL "+
				"buffers cannot be reclaimed", deviceName(s.outputDeviceName))
		}
		s.deviceSink = nil
	}
}

func (s *Stream) StartSource(inputDevice *string) error {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	if s.sourceStop != nil {
		return ErrState
	}
	if s.deviceSource == nil {
		return fmt.Errorf("%w: capture device %q is unavailable", ErrMic, deviceName(s.inputDeviceName))
	}
	s.deviceSource.CaptureStart()
	if err := s.deviceSource.Err(); err != nil {
		return fmt.Errorf("%w: starting capture device %q: %v", ErrMic, deviceName(s.inputDeviceName), err)
	}
	stop := make(chan bool)
	done := make(chan struct{})
	s.sourceStop, s.sourceDone = stop, done
	go s.sourceRoutine(inputDevice, stop, done)
	return nil
}

func (s *Stream) StopSource() error {
	s.sourceMu.Lock()
	if s.sourceStop == nil {
		s.sourceMu.Unlock()
		return ErrState
	}
	stop, done := s.sourceStop, s.sourceDone
	s.sourceStop, s.sourceDone = nil, nil
	close(stop)
	s.sourceMu.Unlock()
	// The routine owns capture access; wait for it before closing/reusing it.
	<-done
	if s.deviceSource == nil {
		return fmt.Errorf("%w: capture device %q is unavailable", ErrMic, deviceName(s.inputDeviceName))
	}
	s.deviceSource.CaptureStop()
	if err := s.deviceSource.Err(); err != nil {
		return fmt.Errorf("%w: stopping capture device %q: %v", ErrMic, deviceName(s.inputDeviceName), err)
	}
	return nil
}

func (s *Stream) GetMicVolume() float32 {
	return math.Float32frombits(s.micVolume.Load())
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
	s.micVolume.Store(math.Float32bits(val))
}

func (s *Stream) OnAudioStream(e *gumble.AudioStreamEvent) {
	s.streamWG.Add(1)
	go func(e *gumble.AudioStreamEvent) {
		defer s.streamWG.Done()
		log.Info("audio stream started for user %s", e.User.Name)
		var source openal.Source
		var emptyBufs openal.Buffers
		var raw [maxBufferSize]byte
		s.render(func() {
			source = openal.NewSource()
			e.User.SetAudioSource(&source)
			if e.User.LocallyMuted() {
				source.SetGain(0)
			} else {
				source.SetGain(e.User.Volume())
			}
			bufferCount := e.Client.Config.Buffers
			if bufferCount < 64 {
				bufferCount = 64
			}
			log.Info("OnAudioStream: creating %d buffers for %s (volume=%.2f gain=%.2f)",
				bufferCount, e.User.Name, e.User.Volume(), source.GetGain())
			emptyBufs = openal.NewBuffers(bufferCount)
		})

		var reclaimLogCounter int
		reclaim := func() {
			s.render(func() {
				processed := source.BuffersProcessed()
				queued := source.BuffersQueued()
				srcState := source.State()
				if processed > 0 {
					reclaimedBufs := make(openal.Buffers, processed)
					source.UnqueueBuffers(reclaimedBufs)
					emptyBufs = append(emptyBufs, reclaimedBufs...)
				}
				reclaimLogCounter++
				// Log every 50th reclaim, or if state is not Playing
				if reclaimLogCounter%50 == 1 || srcState != openal.Playing {
					log.Debug("reclaim #%d: state=%s processed=%d queued=%d empty=%d",
						reclaimLogCounter, srcState, processed, queued, len(emptyBufs))
				}
				if oe := openal.Err(); oe != nil {
					log.Error("reclaim: OpenAL error: %v", oe)
				}
			})
		}

		// Jitter buffer: collects incoming packets, reorders by
		// sequence number, and releases them after a small initial delay.
		var jitterBuf []*gumble.AudioPacket
		var jitterDuration time.Duration
		var jitterNextSeq int64
		var jitterInit, jitterStarted bool
		var jitterLateRun int
		var jitterDrainLogCounter, jitterAnomalyLogCounter int
		resetJitter := func() {
			jitterBuf = nil
			jitterDuration = 0
			jitterNextSeq = 0
			jitterInit = false
			jitterStarted = false
			jitterLateRun = 0
		}

		// insertSorted inserts a packet into the jitter buffer sorted
		// by sequence number.
		insertSorted := func(p *gumble.AudioPacket) {
			// Drop if we already have too many (protect against memory bloat)
			if len(jitterBuf) >= jitterMaxPackets {
				return
			}
			// Find insertion point (ascending sequence order)
			i := 0
			for i < len(jitterBuf) && jitterBuf[i].Sequence < p.Sequence {
				i++
			}
			// Don't insert duplicates
			if i < len(jitterBuf) && jitterBuf[i].Sequence == p.Sequence {
				return
			}
			jitterBuf = append(jitterBuf, nil)
			copy(jitterBuf[i+1:], jitterBuf[i:])
			jitterBuf[i] = p
			jitterDuration += audioPacketDuration(p)
		}

		// popNext removes and returns the packet with the expected next
		// sequence number, or nil if not yet available.
		popNext := func() *gumble.AudioPacket {
			if len(jitterBuf) == 0 || jitterBuf[0].Sequence != jitterNextSeq {
				return nil
			}
			p := jitterBuf[0]
			jitterBuf = jitterBuf[1:]
			jitterDuration -= audioPacketDuration(p)
			// Frame numbers are Mumble timestamps in 10 ms units.
			// Compute the actual step from the PCM sample count so we
			// never skip a legitimate gap.
			samples := len(p.AudioBuffer)
			if samples > gumble.AudioDefaultFrameSize && samples%2 == 0 {
				// Stereo: step = stereo frames / base frame size
				step := int64((samples / 2) / gumble.AudioDefaultFrameSize)
				if step >= 1 {
					jitterNextSeq = p.Sequence + step
				} else {
					jitterNextSeq = p.Sequence + 1
				}
			} else {
				step := int64(samples / gumble.AudioDefaultFrameSize)
				if step >= 1 {
					jitterNextSeq = p.Sequence + step
				} else {
					jitterNextSeq = p.Sequence + 1
				}
			}
			return p
		}

		for packet := range e.C {
			// A talk burst may restart its frame numbers from zero. Reset before
			// testing local mute so an unmute cannot retain the previous burst's
			// timestamp and discard the new burst as permanently late.
			if packet.Terminator {
				resetJitter()
				continue
			}

			// Skip processing if user is locally muted
			if e.User.LocallyMuted() {
				continue
			}

			// Insert into jitter buffer
			insertSorted(packet)

			// Initialize the expected sequence on first packet
			if !jitterInit {
				jitterNextSeq = jitterBuf[0].Sequence
				jitterInit = true
			}

			// Hold only the initial packets. Once playback starts, drain every
			// ready packet so the renderer is fed continuously rather than in
			// bursts of packets.
			if !jitterPlaybackReady(jitterStarted, jitterDuration, e.Client.Config.IncomingAudioBuffer) {
				continue
			}
			jitterStarted = true

			// Drain all packets that are ready (in sequence order)
			for {
				pkt := popNext()
				if pkt == nil {
					if len(jitterBuf) > 0 {
						if jitterBuf[0].Sequence < jitterNextSeq {
							jitterLateRun++
							if jitterShouldResync(jitterLateRun, jitterNextSeq-jitterBuf[0].Sequence) {
								// The sender restarted its frame numbering
								// mid-burst. Follow it instead of discarding
								// every remaining packet until it unkeys.
								log.Debug("jitter: sequence restart for %s, resyncing from %d to %d",
									e.User.Name, jitterNextSeq, jitterBuf[0].Sequence)
								jitterNextSeq = jitterBuf[0].Sequence
								jitterLateRun = 0
								continue
							}
							// Late or duplicate: discard so it doesn't
							// permanently block the drain loop.
							jitterAnomalyLogCounter++
							if jitterAnomalyLogCounter <= 3 || jitterAnomalyLogCounter%1000 == 0 {
								log.Debug("jitter: discarding late seq=%d for %s (next=%d buf=%d)",
									jitterBuf[0].Sequence, e.User.Name, jitterNextSeq, len(jitterBuf))
							}
							jitterDuration -= audioPacketDuration(jitterBuf[0])
							jitterBuf = jitterBuf[1:]
							continue
						}
						if jitterBuf[0].Sequence > jitterNextSeq {
							// Gap in sequence: skip ahead so we don't
							// wait forever for a lost packet.
							jitterAnomalyLogCounter++
							if jitterAnomalyLogCounter <= 3 || jitterAnomalyLogCounter%1000 == 0 {
								log.Debug("jitter: seq gap for %s, skipping from %d to %d (buf=%d)",
									e.User.Name, jitterNextSeq, jitterBuf[0].Sequence, len(jitterBuf))
							}
							jitterNextSeq = jitterBuf[0].Sequence
							continue
						}
						// Sequence == jitterNextSeq but popNext returned nil?
						// Shouldn't happen; break to avoid infinite loop.
					}
					break
				}
				jitterLateRun = 0
				jitterDrainLogCounter++
				if jitterDrainLogCounter <= 3 || jitterDrainLogCounter%1000 == 0 {
					log.Debug("jitter: draining seq=%d for %s (buf=%d emptyBufs=%d)",
						pkt.Sequence, e.User.Name, len(jitterBuf), len(emptyBufs))
				}
				reclaim()
				s.render(func() {
					emptyBufs = s.processAudioPacket(pkt, e.User, &source, emptyBufs, &raw)
				})
			}
		}

		// Drain remaining buffered packets on stream close
		for len(jitterBuf) > 0 {
			pkt := popNext()
			if pkt == nil {
				// Gap in sequence at end; skip
				jitterNextSeq = jitterBuf[0].Sequence
				pkt = popNext()
			}
			if pkt != nil {
				reclaim()
				s.render(func() {
					emptyBufs = s.processAudioPacket(pkt, e.User, &source, emptyBufs, &raw)
				})
			}
		}
		reclaim()
		s.render(func() {
			// OpenAL does not delete buffers when a source is deleted. Reclaim
			// queued buffers after stopping so every generated buffer is freed.
			source.Stop()
			if n := source.BuffersQueued(); n > 0 {
				queuedBufs := make(openal.Buffers, n)
				source.UnqueueBuffers(queuedBufs)
				emptyBufs = append(emptyBufs, queuedBufs...)
			}
			source.Delete()
			emptyBufs.Delete()
			e.User.SetAudioSource(nil)
		})
		log.Debug("audio stream ended for user %s", e.User.Name)
	}(e)
}

func applyVolumeAdjustment(sample int16, adjustment float32) int16 {
	if adjustment == 0 || adjustment == 1 {
		return sample
	}
	adjusted := float32(sample) * adjustment
	if adjusted > 32767 {
		return 32767
	}
	if adjusted < -32768 {
		return -32768
	}
	return int16(adjusted)
}

// processAudioPacket decodes and queues a single audio packet for playback.
// Returns the updated emptyBufs slice after consuming a buffer.
// The caller must call reclaim() before invoking this to ensure buffers
// are available.
func (s *Stream) processAudioPacket(packet *gumble.AudioPacket, user *gumble.User, source *openal.Source, emptyBufs openal.Buffers, raw *[maxBufferSize]byte) openal.Buffers {
	samples := len(packet.AudioBuffer)
	if samples > cap(*raw)/2 {
		return emptyBufs
	}

	boost := user.Boost()
	userVolume := user.Volume()
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
			sample := applyVolumeAdjustment(packet.AudioBuffer[i], packet.VolumeAdjustment)
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
				recordBuffer[recordPtr] = scaleForRecording(sample, userVolume)
				recordPtr++
			}
			binary.LittleEndian.PutUint16((*raw)[rawPtr:], uint16(sample))
			rawPtr += 2

			// Process right channel with saturation protection
			sample = applyVolumeAdjustment(packet.AudioBuffer[i+1], packet.VolumeAdjustment)
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
				recordBuffer[recordPtr] = scaleForRecording(sample, userVolume)
				recordPtr++
			}
			binary.LittleEndian.PutUint16((*raw)[rawPtr:], uint16(sample))
			rawPtr += 2
		}
	} else {
		// Process mono samples with saturation protection
		for i := 0; i < samples; i++ {
			sample := applyVolumeAdjustment(packet.AudioBuffer[i], packet.VolumeAdjustment)
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
				recordSample := scaleForRecording(sample, userVolume)
				recordBuffer[recordPtr] = recordSample
				recordBuffer[recordPtr+1] = recordSample
				recordPtr += 2
			}
			binary.LittleEndian.PutUint16((*raw)[rawPtr:], uint16(sample))
			rawPtr += 2
		}
	}
	if recorder != nil && recordPtr > 0 {
		recorder.RecordAudioFrame(user.Session, recordBuffer[:recordPtr], true)
	}

	if len(emptyBufs) == 0 {
		log.Warn("processAudioPacket: NO EMPTY BUFFERS for %s seq=%d — audio packet dropped!", user.Name, packet.Sequence)
		return emptyBufs
	}

	last := len(emptyBufs) - 1
	buffer := emptyBufs[last]
	emptyBufs[last] = 0
	emptyBufs = emptyBufs[:last]

	buffer.SetData(format, (*raw)[:rawPtr], gumble.AudioSampleRate)
	if oe := openal.Err(); oe != nil {
		log.Error("processAudioPacket: Buffer.SetData error for %s seq=%d: %v", user.Name, packet.Sequence, oe)
	}
	source.QueueBuffer(buffer)
	if oe := openal.Err(); oe != nil {
		log.Error("processAudioPacket: QueueBuffer error for %s seq=%d: %v", user.Name, packet.Sequence, oe)
	}

	srcState := source.State()
	if srcState != openal.Playing {
		log.Debug("processAudioPacket: source state=%s (not playing), calling Play() for %s seq=%d bufs=%d", srcState, user.Name, packet.Sequence, len(emptyBufs))
		source.Play()
		if oe := openal.Err(); oe != nil {
			log.Error("processAudioPacket: Source.Play error for %s seq=%d: %v", user.Name, packet.Sequence, oe)
		}
		log.Debug("processAudioPacket: after Play(), state=%s for %s seq=%d", source.State(), user.Name, packet.Sequence)
	}
	return emptyBufs
}

func (s *Stream) sourceRoutine(inputDevice *string, stop chan bool, done chan struct{}) {
	defer close(done)
	log.Info("source routine started: interval=%v frameSize=%d channels=%d",
		s.client.Config.AudioInterval, s.client.Config.AudioFrameSize(), s.sourceChannels)
	interval := s.client.Config.AudioInterval
	frameSize := s.client.Config.AudioFrameSize()

	devName := ""
	if inputDevice != nil {
		devName = *inputDevice
	}

	reopened := false
	if frameSize != s.sourceFrameSize {
		s.deviceSource.CaptureCloseDevice()
		reopened = true
		s.sourceFrameSize = frameSize
		captureBufferSize := uint32(s.sourceFrameSize * 4)
		s.deviceSource = openal.CaptureOpenDevice(devName, gumble.AudioSampleRate, s.sourceFormat, captureBufferSize)
		if s.deviceSource == nil && s.sourceFormat == openal.FormatStereo16 {
			s.sourceFormat = openal.FormatMono16
			s.sourceChannels = 1
			s.deviceSource = openal.CaptureOpenDevice(devName, gumble.AudioSampleRate, s.sourceFormat, captureBufferSize)
		}
	}
	if s.deviceSource == nil {
		return
	}
	// Reopening after an interval change creates a stopped capture device.
	if reopened {
		s.deviceSource.CaptureStart()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	outgoing := s.client.AudioOutgoing()
	defer close(outgoing)

	var micFailed bool
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			sampleCount := frameSize * s.sourceChannels
			int16Buffer := make([]int16, sampleCount)

			// alcCaptureSamples requires the requested frames to already be
			// available. PipeWire and PulseAudio do not guarantee that a Go
			// ticker fires precisely on a capture-frame boundary.
			hasMicInput := false
			available := s.deviceSource.CapturedSamples()
			var buff []byte
			if available >= uint32(frameSize) {
				buff = s.deviceSource.CaptureSamples(uint32(frameSize))
			}
			if len(buff) == sampleCount*2 {
				hasMicInput = true
				if micFailed {
					micFailed = false
					if s.errorFunc != nil {
						s.errorFunc(nil) // nil signals recovery
					}
				}
				for i := 0; i < sampleCount; i++ {
					sample := int16(binary.LittleEndian.Uint16(buff[i*2:]))
					vol := s.GetMicVolume()
					if vol != 1.0 {
						sample = int16(float32(sample) * vol)
					}
					int16Buffer[i] = sample
				}

				if s.sourceChannels == 1 {
					s.processMonoSamples(int16Buffer)
				} else {
					s.processStereoSamples(int16Buffer, frameSize)
				}
			} else if available >= uint32(frameSize) && !micFailed {
				micFailed = true
				if s.errorFunc != nil {
					s.errorFunc(ErrMic)
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
					recorder.RecordAudioFrame(recorderOutgoingSource, outputBuffer, true)
				}
			} else if hasMicInput {
				// Send mic when no file is playing. If the microphone is
				// stereo, downmix to mono since Mumble voice transmission
				// uses mono Opus encoding.
				outBuf := int16Buffer
				if s.sourceChannels == 2 {
					monoBuf := make([]int16, frameSize)
					for i := 0; i < frameSize; i++ {
						// Average left and right channels
						monoBuf[i] = int16((int32(int16Buffer[i*2]) + int32(int16Buffer[i*2+1])) / 2)
					}
					outBuf = monoBuf
				}
				outgoing <- gumble.AudioBuffer(outBuf)
				if recorder := s.getRecorder(); recorder != nil {
					recorder.RecordAudioFrame(recorderOutgoingSource, outBuf, false)
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
	s.processChannel(samples, s.noiseProcessor, s.micAGC)
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

	s.processChannel(left, s.noiseProcessor, s.micAGC)
	s.processChannel(right, s.noiseProcessorRight, s.micAGCRight)

	for i := 0; i < frameSize; i++ {
		idx := i * 2
		samples[idx] = left[i]
		samples[idx+1] = right[i]
	}
}

func (s *Stream) processChannel(samples []int16, noiseProcessor NoiseProcessor, micAGC *audio.AGC) {
	if noiseProcessor != nil && noiseProcessor.IsEnabled() {
		noiseProcessor.ProcessSamples(samples)
	}
	if micAGC != nil && micAGC.IsEnabled() {
		micAGC.ProcessSamples(samples)
	}
}

func (s *Stream) ensureStereoProcessors() {
	if s.micAGCRight == nil {
		s.micAGCRight = audio.NewAGC()
		if s.micAGC != nil {
			s.micAGCRight.SetEnabled(s.micAGC.IsEnabled())
		}
	}
	if s.noiseProcessorRight == nil {
		s.noiseProcessorRight = cloneNoiseProcessor(s.noiseProcessor)
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
