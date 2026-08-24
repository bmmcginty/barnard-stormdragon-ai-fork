package recording

import "testing"

// Regression: the per-source mix queues grew without bound. The tick drains a
// fixed chunk per source, so a stalled encoder leaves a deficit the loop never
// makes up, and the backlog only ever grew from there.
func TestRecorderQueueIsCapped(t *testing.T) {
	t.Parallel()

	var queue []int16
	frame := make([]int16, 960)
	// Far more audio than the encoder could have consumed.
	for i := 0; i < 2000; i++ {
		queue = appendCapped(queue, frame)
	}

	if len(queue) > maxQueuedSamples {
		t.Fatalf("queue grew to %d samples, above the %d cap",
			len(queue), maxQueuedSamples)
	}
}

// Capping must keep the newest audio: dropping the newest would make the
// recording lag further behind with every overflow.
func TestRecorderQueueKeepsNewestAudio(t *testing.T) {
	t.Parallel()

	var queue []int16
	// Fill past the cap with a marker in the final frame.
	filler := make([]int16, maxQueuedSamples)
	queue = appendCapped(queue, filler)
	newest := []int16{1, 2, 3, 4}
	queue = appendCapped(queue, newest)

	if len(queue) != maxQueuedSamples {
		t.Fatalf("expected the queue to sit at the %d cap, got %d",
			maxQueuedSamples, len(queue))
	}
	tail := queue[len(queue)-len(newest):]
	for i, want := range newest {
		if tail[i] != want {
			t.Fatalf("newest audio was dropped: tail %v, want %v", tail, newest)
		}
	}
}
