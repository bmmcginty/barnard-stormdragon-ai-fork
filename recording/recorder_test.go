package recording

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type trackingWriteCloser struct{ closed bool }

func (w *trackingWriteCloser) Write([]byte) (int, error) { return 0, nil }
func (w *trackingWriteCloser) Close() error              { w.closed = true; return nil }

var _ io.WriteCloser = (*trackingWriteCloser)(nil)

// Regression: Stop closed ffmpeg stdin while the worker could still write,
// creating a spurious closed-pipe recording failure.
func TestStopLeavesEncoderClosureToWorker(t *testing.T) {
	stdin := &trackingWriteCloser{}
	done := make(chan struct{})
	close(done)
	r := &Recorder{stdin: stdin, stop: make(chan struct{}), done: done}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if stdin.closed {
		t.Fatal("Stop closed stdin instead of the worker")
	}
}

func TestNormalizeFormat(t *testing.T) {
	tests := map[string]string{
		"":       "flac",
		" FLAC ": "flac",
		".opus":  "opus",
	}
	for input, want := range tests {
		if got := NormalizeFormat(input); got != want {
			t.Fatalf("NormalizeFormat(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUniquePathAvoidsCollision(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 14, 12, 30, 0, 0, time.Local)
	first := filepath.Join(dir, "barnard-recording-20260514-123000.flac")
	if err := os.WriteFile(first, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	got := UniquePath(dir, now, "flac")
	want := filepath.Join(dir, "barnard-recording-20260514-123000-2.flac")
	if got != want {
		t.Fatalf("UniquePath() = %q, want %q", got, want)
	}
}

func TestReservePathPreventsConcurrentRecordingCollisions(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 14, 12, 30, 0, 0, time.Local)
	paths := make(chan string, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := reservePath(dir, now, "flac")
			if err != nil {
				errs <- err
				return
			}
			paths <- path
		}()
	}
	wg.Wait()
	close(paths)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var reserved []string
	for path := range paths {
		reserved = append(reserved, path)
	}
	if len(reserved) != 2 || reserved[0] == reserved[1] {
		t.Fatalf("reserved paths = %#v", reserved)
	}
}

func TestNormalizeStereoFrame(t *testing.T) {
	// Mono input duplicating each sample to both channels.
	mono := NormalizeStereoFrame([]int16{1, -2, 3}, false)
	wantMono := []int16{1, 1, -2, -2, 3, 3}
	if len(mono) != len(wantMono) {
		t.Fatalf("mono len = %d, want %d", len(mono), len(wantMono))
	}
	for i := range wantMono {
		if mono[i] != wantMono[i] {
			t.Fatalf("mono[%d] = %d, want %d", i, mono[i], wantMono[i])
		}
	}

	// Even-length mono must not be mistaken for stereo.
	monoEven := NormalizeStereoFrame([]int16{1, -2}, false)
	wantMonoEven := []int16{1, 1, -2, -2}
	if len(monoEven) != len(wantMonoEven) {
		t.Fatalf("monoEven len = %d, want %d", len(monoEven), len(wantMonoEven))
	}
	for i := range wantMonoEven {
		if monoEven[i] != wantMonoEven[i] {
			t.Fatalf("monoEven[%d] = %d, want %d", i, monoEven[i], wantMonoEven[i])
		}
	}

	// Stereo input passes through unchanged.
	stereo := NormalizeStereoFrame([]int16{1, 2, 3, 4, 5, 6}, true)
	wantStereo := []int16{1, 2, 3, 4, 5, 6}
	if len(stereo) != len(wantStereo) {
		t.Fatalf("stereo len = %d, want %d", len(stereo), len(wantStereo))
	}
	for i := range wantStereo {
		if stereo[i] != wantStereo[i] {
			t.Fatalf("stereo[%d] = %d, want %d", i, stereo[i], wantStereo[i])
		}
	}
}

func TestMixSaturates(t *testing.T) {
	dst := []int16{32000, -32000, 10}
	mix(dst, []int16{2000, -2000, -20})
	want := []int16{32767, -32768, -10}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("dst[%d] = %d, want %d", i, dst[i], want[i])
		}
	}
}
