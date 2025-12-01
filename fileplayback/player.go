package fileplayback

import (
	"encoding/binary"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/gumble/go-openal/openal"
)

// Player handles file playback and mixing with microphone audio
type Player struct {
	client      *gumble.Client
	filename    string
	audioChan   chan gumble.AudioBuffer
	stopChan    chan struct{}
	mutex       sync.Mutex
	playing     bool
	errorFunc   func(error)

	// Local playback
	localSource *openal.Source
	localBuffers openal.Buffers
}

// New creates a new file player
func New(client *gumble.Client) *Player {
	return &Player{
		client:    client,
		audioChan: make(chan gumble.AudioBuffer, 100),
		stopChan:  make(chan struct{}),
	}
}

// SetErrorFunc sets the error callback function
func (p *Player) SetErrorFunc(f func(error)) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.errorFunc = f
}

func (p *Player) reportError(err error) {
	p.mutex.Lock()
	errorFunc := p.errorFunc
	p.mutex.Unlock()

	if errorFunc != nil {
		errorFunc(err)
	}
}

// PlayFile starts playing a file
func (p *Player) PlayFile(filename string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.playing {
		return errors.New("file already playing")
	}

	p.filename = filename

	// Initialize local playback
	source := openal.NewSource()
	p.localSource = &source
	p.localSource.SetGain(1.0)

	// Create buffers for local playback
	p.localBuffers = openal.NewBuffers(64)

	// Start the file reading goroutine
	p.playing = true
	p.stopChan = make(chan struct{})
	go p.readFileAudio()

	return nil
}

// Stop stops the currently playing file
func (p *Player) Stop() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.playing {
		return errors.New("no file playing")
	}

	close(p.stopChan)
	p.playing = false

	// Clean up local playback
	if p.localSource != nil {
		p.localSource.Stop()
		p.localSource.Delete()
		p.localSource = nil
	}
	if p.localBuffers != nil {
		p.localBuffers.Delete()
		p.localBuffers = nil
	}

	// Drain the audio channel
	for len(p.audioChan) > 0 {
		<-p.audioChan
	}

	return nil
}

// IsPlaying returns true if a file is currently playing
func (p *Player) IsPlaying() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.playing
}

// GetAudioFrame returns the next audio frame from the file, or nil if no file is playing
func (p *Player) GetAudioFrame() []int16 {
	select {
	case frame := <-p.audioChan:
		return []int16(frame)
	default:
		return nil
	}
}

// playLocalAudio plays audio through the local OpenAL source
func (p *Player) playLocalAudio(data []byte) {
	if p.localSource == nil {
		return
	}

	// Reclaim processed buffers
	if n := p.localSource.BuffersProcessed(); n > 0 {
		reclaimedBufs := make(openal.Buffers, n)
		p.localSource.UnqueueBuffers(reclaimedBufs)
		p.localBuffers = append(p.localBuffers, reclaimedBufs...)
	}

	// If we have available buffers, queue more audio
	if len(p.localBuffers) > 0 {
		buffer := p.localBuffers[len(p.localBuffers)-1]
		p.localBuffers = p.localBuffers[:len(p.localBuffers)-1]

		// Set buffer data as stereo
		buffer.SetData(openal.FormatStereo16, data, gumble.AudioSampleRate)
		p.localSource.QueueBuffer(buffer)

		// Start playing if not already
		if p.localSource.State() != openal.Playing {
			p.localSource.Play()
		}
	}
}

// readFileAudio reads audio from the file via ffmpeg
func (p *Player) readFileAudio() {
	interval := p.client.Config.AudioInterval
	frameSize := p.client.Config.AudioFrameSize()

	// Use stereo output from ffmpeg to preserve stereo files
	// Add -loglevel error to suppress info messages
	args := []string{"-loglevel", "error", "-i", p.filename}
	args = append(args, "-ac", "2", "-ar", strconv.Itoa(gumble.AudioSampleRate), "-f", "s16le", "-")

	cmd := exec.Command("ffmpeg", args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		p.mutex.Lock()
		p.playing = false
		p.mutex.Unlock()
		p.reportError(errors.New("failed to create ffmpeg pipe: " + err.Error()))
		return
	}

	if err := cmd.Start(); err != nil {
		p.mutex.Lock()
		p.playing = false
		p.mutex.Unlock()
		p.reportError(errors.New("failed to start ffmpeg: " + err.Error()))
		return
	}

	// Stereo has 2 channels, so we need twice the buffer size
	byteBuffer := make([]byte, frameSize*2*2) // frameSize * 2 channels * 2 bytes per sample

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			cmd.Process.Kill()
			cmd.Wait()
			return
		case <-ticker.C:
			n, err := io.ReadFull(pipe, byteBuffer)
			if err != nil || n != len(byteBuffer) {
				// File finished playing
				p.mutex.Lock()
				p.playing = false
				// Clean up local playback
				if p.localSource != nil {
					p.localSource.Stop()
					p.localSource.Delete()
					p.localSource = nil
				}
				if p.localBuffers != nil {
					p.localBuffers.Delete()
					p.localBuffers = nil
				}
				p.mutex.Unlock()
				cmd.Wait()
				// Notify that file finished
				p.reportError(errors.New("file playback finished"))
				return
			}

			// Convert stereo bytes to int16 buffer
			int16Buffer := make([]int16, frameSize*2) // stereo
			for i := 0; i < len(int16Buffer); i++ {
				int16Buffer[i] = int16(binary.LittleEndian.Uint16(byteBuffer[i*2 : (i+1)*2]))
			}

			// Play locally through OpenAL
			p.playLocalAudio(byteBuffer[:n])

			// Send to channel (non-blocking)
			select {
			case p.audioChan <- gumble.AudioBuffer(int16Buffer):
			default:
				// Channel full, skip this frame
			}
		}
	}
}
