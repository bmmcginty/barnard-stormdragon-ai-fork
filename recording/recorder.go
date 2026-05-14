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
	path := UniquePath(directory, now, format)
	args := ffmpegArgs(format, path)
	cmd := exec.Command("ffmpeg", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
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

func (r *Recorder) RecordAudioFrame(source uint32, samples []int16) {
	if r == nil || len(samples) == 0 {
		return
	}
	if len(r.input) >= cap(r.input) {
		return
	}
	frame := NormalizeStereoFrame(samples, r.frameSize)
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
		close(r.stop)
		if r.stdin != nil {
			r.stdin.Close()
		}
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

func (r *Recorder) run() {
	defer close(r.done)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	queues := make(map[uint32][][]int16)
	frame := make([]int16, r.frameSize*gumble.AudioChannels)
	for {
		select {
		case <-r.stop:
			r.closeEncoder()
			return
		case item := <-r.input:
			queues[item.source] = append(queues[item.source], item.samples)
		case <-ticker.C:
			clear(frame)
			for source, queue := range queues {
				if len(queue) == 0 {
					delete(queues, source)
					continue
				}
				mix(frame, queue[0])
				queue = queue[1:]
				if len(queue) == 0 {
					delete(queues, source)
				} else {
					queues[source] = queue
				}
			}
			if err := writePCM(r.stdin, frame); err != nil {
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

func NormalizeStereoFrame(samples []int16, frameSize int) []int16 {
	out := make([]int16, frameSize*gumble.AudioChannels)
	if len(samples) >= frameSize*gumble.AudioChannels && len(samples)%gumble.AudioChannels == 0 {
		copy(out, samples[:frameSize*gumble.AudioChannels])
		return out
	}
	limit := frameSize
	if len(samples) < limit {
		limit = len(samples)
	}
	for i := 0; i < limit; i++ {
		out[i*2] = samples[i]
		out[i*2+1] = samples[i]
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

func ffmpegArgs(format string, path string) []string {
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
	return append(args, "-y", path)
}
