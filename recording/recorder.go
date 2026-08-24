package recording

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble"
)

const (
	FormatFLAC = "flac"
	FormatOpus = "opus"
)

// maxQueuedSamples bounds the per-source mix backlog at roughly five seconds
// of 48 kHz stereo audio. A source that runs further ahead than this is ahead
// because the encoder stalled, and no amount of retained audio recovers the
// timeline; keeping the newest is better than growing without bound.
const maxQueuedSamples = 5 * gumble.AudioSampleRate * gumble.AudioChannels

type Recorder struct {
	path      string
	format    string
	frameSize int
	interval  time.Duration

	cmd   *exec.Cmd
	stdin io.WriteCloser

	input chan sourceFrame
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once

	mu  sync.Mutex
	err error
}

type sourceFrame struct {
	source  uint32
	samples []int16
}

func New(directory string, format string, now time.Time, frameSize int, interval time.Duration) (*Recorder, error) {
	format = NormalizeFormat(format)
	if format != FormatFLAC && format != FormatOpus {
		return nil, fmt.Errorf("unsupported recording format %q", format)
	}
	if frameSize <= 0 {
		return nil, errors.New("invalid recording frame size")
	}
	if interval <= 0 {
		interval = gumble.AudioDefaultInterval
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, err
	}
	output, path, err := reserveOutput(directory, now, format)
	if err != nil {
		return nil, err
	}
	args := ffmpegArgs(format)
	cmd := exec.Command("ffmpeg", args...)
	// Pass the reserved file descriptor directly to ffmpeg. The file is never
	// reopened by pathname, preventing replacement between reservation and use.
	cmd.ExtraFiles = []*os.File{output}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = output.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = output.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := output.Close(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.Remove(path)
		return nil, err
	}
	recorder := &Recorder{
		path:      path,
		format:    format,
		frameSize: frameSize,
		interval:  interval,
		cmd:       cmd,
		stdin:     stdin,
		input:     make(chan sourceFrame, 512),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go recorder.run()
	return recorder, nil
}

func NormalizeFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	format = strings.TrimPrefix(format, ".")
	if format == "" {
		return FormatFLAC
	}
	return format
}

func reserveOutput(directory string, now time.Time, format string) (*os.File, string, error) {
	for {
		path := UniquePath(directory, now, format)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return file, path, nil
	}
}

// reservePath remains available for callers that only need to reserve a name.
func reservePath(directory string, now time.Time, format string) (string, error) {
	file, path, err := reserveOutput(directory, now, format)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func UniquePath(directory string, now time.Time, format string) string {
	base := fmt.Sprintf("barnard-recording-%s", now.Format("20060102-150405"))
	path := filepath.Join(directory, base+"."+format)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	for i := 2; ; i++ {
		path = filepath.Join(directory, fmt.Sprintf("%s-%d.%s", base, i, format))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
	}
}

func (r *Recorder) Path() string {
	return r.path
}

func (r *Recorder) RecordAudioFrame(source uint32, samples []int16, stereo bool) {
	if r == nil || len(samples) == 0 {
		return
	}
	if len(r.input) >= cap(r.input) {
		return
	}
	frame := NormalizeStereoFrame(samples, stereo)
	select {
	case r.input <- sourceFrame{source: source, samples: frame}:
	default:
	}
}

func (r *Recorder) Stop() error {
	if r == nil {
		return nil
	}
	r.once.Do(func() {
		// run owns stdin and closes it only after it has stopped writing.
		// Closing it here races writePCM and turns a normal stop into EPIPE.
		close(r.stop)
	})
	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
		if r.cmd != nil && r.cmd.Process != nil {
			r.cmd.Process.Kill()
		}
		<-r.done
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// appendCapped adds a source's incoming samples to its mix queue, bounded at
// maxQueuedSamples. Each tick drains one fixed chunk per source, so a stalled
// encoder leaves a deficit the loop never makes up and the backlog would
// otherwise grow for as long as the recording ran. The newest audio is kept:
// discarding it instead would only push the recording further behind.
func appendCapped(queue []int16, incoming []int16) []int16 {
	queue = append(queue, incoming...)
	if len(queue) > maxQueuedSamples {
		queue = append(queue[:0], queue[len(queue)-maxQueuedSamples:]...)
	}
	return queue
}

func (r *Recorder) run() {
	defer close(r.done)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	// Per-source accumulated stereo samples. Incoming frames of any size are
	// appended and then consumed in frameSize*AudioChannels chunks each tick.
	queues := make(map[uint32][]int16)
	chunkSize := r.frameSize * gumble.AudioChannels
	chunk := make([]int16, chunkSize)
	for {
		select {
		case <-r.stop:
			r.closeEncoder()
			return
		case item := <-r.input:
			queues[item.source] = appendCapped(queues[item.source], item.samples)
		case <-ticker.C:
			clear(chunk)
			for source, buffer := range queues {
				if len(buffer) == 0 {
					delete(queues, source)
					continue
				}
				// Mix one chunk worth of samples from this source.
				if len(buffer) <= chunkSize {
					mix(chunk, buffer)
					delete(queues, source)
				} else {
					mix(chunk, buffer[:chunkSize])
					queues[source] = buffer[chunkSize:]
				}
			}
			if err := writePCM(r.stdin, chunk); err != nil {
				r.setError(err)
				r.closeEncoder()
				return
			}
		}
	}
}

func (r *Recorder) closeEncoder() {
	if r.stdin != nil {
		if err := r.stdin.Close(); err != nil {
			if !errors.Is(err, os.ErrClosed) {
				r.setError(err)
			}
		}
	}
	if r.cmd != nil {
		if err := r.cmd.Wait(); err != nil {
			r.setError(err)
		}
	}
}

func (r *Recorder) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = err
	}
}

// NormalizeStereoFrame ensures samples are in stereo interleaved format.
// If stereo is true the samples are returned as-is (already interleaved).
// Mono input is duplicated to both channels. The returned slice preserves
// all input audio without truncation.
func NormalizeStereoFrame(samples []int16, stereo bool) []int16 {
	if stereo {
		return samples
	}
	// Convert mono to stereo by duplicating each sample.
	out := make([]int16, len(samples)*gumble.AudioChannels)
	for i, s := range samples {
		out[i*2] = s
		out[i*2+1] = s
	}
	return out
}

func mix(dst []int16, src []int16) {
	limit := len(dst)
	if len(src) < limit {
		limit = len(src)
	}
	for i := 0; i < limit; i++ {
		sum := int32(dst[i]) + int32(src[i])
		if sum > 32767 {
			sum = 32767
		} else if sum < -32768 {
			sum = -32768
		}
		dst[i] = int16(sum)
	}
}

func writePCM(w io.Writer, samples []int16) error {
	buf := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(sample))
	}
	_, err := w.Write(buf)
	return err
}

func ffmpegArgs(format string) []string {
	args := []string{
		"-loglevel", "error",
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", gumble.AudioSampleRate),
		"-ac", fmt.Sprintf("%d", gumble.AudioChannels),
		"-i", "pipe:0",
	}
	if format == FormatOpus {
		args = append(args, "-c:a", "libopus")
	}
	return append(args, "-f", format, "pipe:3")
}
