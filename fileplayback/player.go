package fileplayback

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble"
)

// Player handles file playback and mixing with microphone audio
type Player struct {
	client    *gumble.Client
	filename  string
	audioChan chan gumble.AudioBuffer
	stopChan  chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	cmd       *exec.Cmd
	mutex     sync.Mutex
	wg        sync.WaitGroup
	playing   bool
	stopping  bool
	errorFunc func(error)

	localPlayback func([]byte)
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

// SetLocalPlayback sets the callback that plays file audio locally. The
// callback is called with nil when playback stops and should release resources.
func (p *Player) SetLocalPlayback(f func([]byte)) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.localPlayback = f
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

	// Start the file reading goroutine
	p.playing = true
	p.stopping = false
	p.stopChan = make(chan struct{})
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wg.Add(1)
	go p.readFileAudio()

	return nil
}

// Stop stops the currently playing file
func (p *Player) Stop() error {
	p.mutex.Lock()
	if !p.playing {
		p.mutex.Unlock()
		return errors.New("no file playing")
	}
	if !p.stopping {
		p.stopping = true
		close(p.stopChan)
		if p.cancel != nil {
			p.cancel()
		}
		terminateProcessGroup(p.cmd)
	}
	p.mutex.Unlock()

	// A new PlayFile must not replace session state until ffmpeg and the old
	// worker have exited, otherwise old audio can enter the new playback.
	p.wg.Wait()
	p.mutex.Lock()
	p.playing, p.stopping, p.cancel, p.cmd = false, false, nil, nil
	localPlayback := p.localPlayback
	p.mutex.Unlock()
	if localPlayback != nil {
		localPlayback(nil)
	}
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

func (p *Player) playLocalAudio(data []byte) {
	p.mutex.Lock()
	localPlayback := p.localPlayback
	p.mutex.Unlock()
	if localPlayback != nil {
		localPlayback(data)
	}
}

// readFileAudio reads audio from the file via ffmpeg
func (p *Player) readFileAudio() {
	defer p.wg.Done()
	interval := p.client.Config.AudioInterval
	frameSize := p.client.Config.AudioFrameSize()

	// Use stereo output from ffmpeg to preserve stereo files
	// Add -loglevel error to suppress info messages
	args := []string{"-loglevel", "error", "-i", p.filename}
	args = append(args, "-ac", "2", "-ar", strconv.Itoa(gumble.AudioSampleRate), "-f", "s16le", "-")

	p.mutex.Lock()
	ctx := p.ctx
	p.mutex.Unlock()
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	configureProcessGroup(cmd)
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
	p.mutex.Lock()
	p.cmd = cmd
	p.mutex.Unlock()

	// Stereo has 2 channels, so we need twice the buffer size
	byteBuffer := make([]byte, frameSize*2*2) // frameSize * 2 channels * 2 bytes per sample

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			terminateProcessGroup(cmd)
			cmd.Wait()
			return
		case <-ticker.C:
			n, err := io.ReadFull(pipe, byteBuffer)
			if err != nil || n != len(byteBuffer) {
				select {
				case <-p.stopChan:
					cmd.Wait()
					return
				default:
				}
				// File finished playing.
				p.mutex.Lock()
				p.playing = false
				localPlayback := p.localPlayback
				p.mutex.Unlock()
				if localPlayback != nil {
					localPlayback(nil)
				}
				cmd.Wait()
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
