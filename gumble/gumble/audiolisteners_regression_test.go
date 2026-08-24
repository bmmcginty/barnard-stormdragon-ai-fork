package gumble

import "testing"

type countingAudioListener struct{ streams int }

func (l *countingAudioListener) OnAudioStream(e *AudioStreamEvent) {
	l.streams++
	go func() {
		for range e.C {
		}
	}()
}

// Regression: an audio listener is removed from the shared list only by
// Detach. A stream that was created but never destroyed therefore stayed
// subscribed for the life of the process, and every audio packet from every
// user was dispatched to it as well — one goroutine, one packet queue and one
// set of playback buffers per orphan, per user. This test pins the fan-out
// behaviour that makes failing to detach so expensive.
func TestDispatchAudioFansOutToEveryAttachedListener(t *testing.T) {
	c := &Client{Config: NewConfig(), Users: make(Users)}
	user := c.Users.create(1)

	first := &countingAudioListener{}
	second := &countingAudioListener{}
	firstLink := c.Config.AttachAudio(first)
	c.Config.AttachAudio(second)

	c.dispatchAudio(user, &AudioPacket{Client: c, Sender: user})
	if first.streams != 1 || second.streams != 1 {
		t.Fatalf("expected both listeners to receive the stream, got %d and %d",
			first.streams, second.streams)
	}

	// Detaching must actually stop the fan-out; this is the only thing that
	// keeps a replaced stream from accumulating.
	firstLink.Detach()
	third := &countingAudioListener{}
	c.Config.AttachAudio(third)
	other := c.Users.create(2)
	c.dispatchAudio(other, &AudioPacket{Client: c, Sender: other})

	if first.streams != 1 {
		t.Fatalf("detached listener still received audio: %d streams", first.streams)
	}
	if second.streams != 2 || third.streams != 1 {
		t.Fatalf("attached listeners missed the second user: %d and %d",
			second.streams, third.streams)
	}
}

// Detach must also release the per-user stream channels it owns, so a
// destroyed stream does not pin its queued audio.
func TestDetachClosesPerUserStreams(t *testing.T) {
	c := &Client{Config: NewConfig(), Users: make(Users)}
	user := c.Users.create(1)

	listener := &countingAudioListener{}
	link := c.Config.AttachAudio(listener)
	c.dispatchAudio(user, &AudioPacket{Client: c, Sender: user})

	item := c.Config.AudioListeners.head
	if item == nil || len(item.streams) != 1 {
		t.Fatal("expected one per-user stream before detaching")
	}
	link.Detach()
	if len(item.streams) != 0 {
		t.Fatalf("Detach left %d per-user streams behind", len(item.streams))
	}
}
