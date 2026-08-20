package fileplayback

import (
	"testing"
	"time"
)

// Regression: Stop returned before the previous ffmpeg worker exited, allowing
// a subsequent PlayFile to replace shared state while old audio was still sent.
func TestStopWaitsForPlaybackWorker(t *testing.T) {
	p := &Player{playing: true, stopChan: make(chan struct{})}
	p.wg.Add(1)
	done := make(chan error, 1)
	go func() { done <- p.Stop() }()
	select {
	case <-done:
		t.Fatal("Stop returned before playback worker ended")
	case <-time.After(20 * time.Millisecond):
	}
	p.wg.Done()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
