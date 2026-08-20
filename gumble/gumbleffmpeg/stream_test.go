package gumbleffmpeg

import (
	"testing"
	"time"
)

// Regression: Pause sent on an unbuffered channel after the process had
// exited, leaving callers blocked forever.
func TestPauseDoesNotBlockWhenProcessHasExited(t *testing.T) {
	s := &Stream{state: StatePlaying, pause: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() { done <- s.Pause() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Pause blocked after process exit")
	}
}
