package gumbleopenal

import (
	"errors"
	"strings"
	"testing"
	"time"

	"git.stormux.org/storm/barnard/gumble/go-openal/openal"
	"git.stormux.org/storm/barnard/gumble/gumble"
)

// Regression: audio cleanup could send a final render command after Destroy
// had closed renderCh, panicking instead of safely discarding that work.
// Regression: StopSource returned before the capture worker ended, allowing
// Destroy to close the device while that worker still used it.
// Regression: OpenAL returned only a generic input/output error, hiding the
// actual configured device that a user must correct.
func TestDeviceOpenErrorsIncludeConfiguredDevice(t *testing.T) {
	input := openInputDeviceError("virtual_mic.monitor", openal.FormatMono16)
	if !errors.Is(input, ErrInputDevice) || !strings.Contains(input.Error(), "virtual_mic.monitor") {
		t.Fatalf("input error %q", input)
	}
	output := openOutputDeviceError("")
	if !errors.Is(output, ErrOutputDevice) || !strings.Contains(output.Error(), "default") {
		t.Fatalf("output error %q", output)
	}
}

// Regression: later capture start failures also omitted the configured device.
func TestStartSourceUnavailableDeviceIncludesName(t *testing.T) {
	s := &Stream{inputDeviceName: "virtual_mic.monitor"}
	err := s.StartSource(nil)
	if !errors.Is(err, ErrMic) || !strings.Contains(err.Error(), "virtual_mic.monitor") {
		t.Fatalf("start error %q", err)
	}
}

func TestStopSourceWaitsForWorker(t *testing.T) {
	stop, done := make(chan bool), make(chan struct{})
	s := &Stream{sourceStop: stop, sourceDone: done}
	returned := make(chan struct{})
	go func() { _ = s.StopSource(); close(returned) }()
	select {
	case <-returned:
		t.Fatal("StopSource returned before worker")
	default:
	}
	close(done)
	<-returned
}

func TestJitterPlaybackDelayAppliesOnlyAtStartup(t *testing.T) {
	if jitterPlaybackReady(false, 20*time.Millisecond, 40*time.Millisecond) {
		t.Fatal("jitter playback started before initial buffer filled")
	}
	if !jitterPlaybackReady(false, 40*time.Millisecond, 40*time.Millisecond) {
		t.Fatal("jitter playback did not start after initial buffer filled")
	}
	if !jitterPlaybackReady(true, 0, 40*time.Millisecond) {
		t.Fatal("jitter playback paused while refilling after startup")
	}
}

func TestJitterResyncsAfterSenderRestartsSequence(t *testing.T) {
	// Mumble restarts frame numbering at zero when the sender switches audio
	// devices mid-burst, and sends no terminator to announce it.
	if !jitterShouldResync(jitterLateResync, 52724) {
		t.Fatal("jitter did not resync after the sender restarted its frame numbering")
	}
	if jitterShouldResync(jitterLateResync-1, 52724) {
		t.Fatal("jitter resynced before the late run was conclusive")
	}
	// A clump of reordered packets is bounded and recovers on its own; it must
	// not drag the expected sequence backwards.
	if jitterShouldResync(jitterLateResync, jitterResyncJump-1) {
		t.Fatal("jitter resynced on a backwards jump small enough to be reordering")
	}
	if jitterShouldResync(1, 52724) {
		t.Fatal("jitter resynced on a single late packet")
	}
}

func TestAudioPacketDurationUsesStereoFrameCount(t *testing.T) {
	packet := &gumble.AudioPacket{AudioBuffer: make(gumble.AudioBuffer, 2*gumble.AudioDefaultFrameSize)}
	if got := audioPacketDuration(packet); got != 10*time.Millisecond {
		t.Fatalf("audioPacketDuration = %v, want 10ms", got)
	}
}

func TestRenderRejectsWorkAfterShutdown(t *testing.T) {
	s := &Stream{renderClosed: true}
	called := false
	if s.render(func() { called = true }) {
		t.Fatal("closed renderer accepted work")
	}
	if called {
		t.Fatal("closed renderer executed work")
	}
}
