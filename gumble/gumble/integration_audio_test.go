//go:build integration

package gumble_test

import (
	"crypto/tls"
	"fmt"
	"math"
	"net"
	"os"
	"testing"
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble"
	_ "git.stormux.org/storm/barnard/gumble/opus"
)

type integrationAudioListener struct{ packets chan *gumble.AudioPacket }

func (l *integrationAudioListener) OnAudioStream(e *gumble.AudioStreamEvent) {
	go func() {
		for p := range e.C {
			l.packets <- p
		}
	}()
}

// TestLocalMumbleAudioRoundTrip sends generated 440 Hz audio through a real
// local Mumble server and requires the other client to decode non-silent PCM.
func TestLocalMumbleAudioRoundTrip(t *testing.T) {
	testLocalMumbleAudioRoundTrip(t, false, gumble.AudioDefaultInterval)
}

// Regression: non-default permitted intervals must preserve generated audio
// instead of using the old fixed-10ms bitrate calculation.
func TestLocalMumbleAudioTwentyMilliseconds(t *testing.T) {
	testLocalMumbleAudioRoundTrip(t, false, 20*time.Millisecond)
}

func TestLocalMumbleAudioFortyMilliseconds(t *testing.T) {
	testLocalMumbleAudioRoundTrip(t, false, 40*time.Millisecond)
}

func TestLocalMumbleAudioSixtyMilliseconds(t *testing.T) {
	testLocalMumbleAudioRoundTrip(t, false, 60*time.Millisecond)
}

// Regression: TCP tunnel fallback must remain usable when UDP is deliberately
// disabled or blocked, rather than silently dropping valid audio.
func TestLocalMumbleTCPAudioFallback(t *testing.T) {
	testLocalMumbleAudioRoundTrip(t, true, gumble.AudioDefaultInterval)
}

func testLocalMumbleAudioRoundTrip(t *testing.T, disableUDP bool, interval time.Duration) {
	if os.Getenv("BARNARD_MUMBLE_INTEGRATION") != "1" {
		t.Skip("set BARNARD_MUMBLE_INTEGRATION=1")
	}
	newConfig := func(name string) *gumble.Config {
		c := gumble.NewConfig()
		c.Address = "localhost:64738"
		c.Username = name
		c.DisableUDP = disableUDP
		c.AudioInterval = interval
		return c
	}
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	listener := &integrationAudioListener{packets: make(chan *gumble.AudioPacket, 8)}
	recvConfig := newConfig(fmt.Sprintf("barnard-it-recv-%d", time.Now().UnixNano()))
	recvConfig.AttachAudio(listener)
	receiver, err := gumble.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, recvConfig, tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Disconnect()
	sendConfig := newConfig(fmt.Sprintf("barnard-it-send-%d", time.Now().UnixNano()))
	sender, err := gumble.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, sendConfig, tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Disconnect()
	if sender.AudioEncoder == nil {
		t.Fatal("no negotiated audio encoder")
	}
	time.Sleep(1500 * time.Millisecond)
	if disableUDP {
		if sender.UDPActive() || receiver.UDPActive() {
			t.Fatal("UDP activated despite TCP-only configuration")
		}
	} else if !sender.UDPActive() || !receiver.UDPActive() {
		t.Fatalf("native UDP did not become active: sender=%v receiver=%v", sender.UDPActive(), receiver.UDPActive())
	}
	frame := make([]int16, sendConfig.AudioFrameSize())
	for i := 0; i < 8; i++ {
		for sample := range frame {
			index := i*len(frame) + sample
			frame[sample] = int16(12000 * math.Sin(2*math.Pi*440*float64(index)/gumble.AudioSampleRate))
		}
		raw, err := sender.AudioEncoder.Encode(frame, len(frame), sendConfig.AudioDataBytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := sender.WriteAudio(4, 0, int64(i), i == 7, raw, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Discard the first few decoder warm-up frames before measuring pitch.
	var packet *gumble.AudioPacket
	var previousSequence int64
	frameStep := int64(sendConfig.AudioFrameSize() / gumble.AudioDefaultFrameSize)
	for i := 0; i < 3; i++ {
		select {
		case packet = <-listener.packets:
			if i > 0 && packet.Sequence != previousSequence+frameStep {
				t.Fatalf("choppy relay: sequence gap %d -> %d", previousSequence, packet.Sequence)
			}
			previousSequence = packet.Sequence
		case <-time.After(8 * time.Second):
			t.Fatal("timed out waiting for relayed audio")
		}
	}
	select {
	case packet = <-listener.packets:
		if packet.Sequence != previousSequence+frameStep {
			t.Fatalf("choppy relay: sequence gap %d -> %d", previousSequence, packet.Sequence)
		}
		frequency, purity, peak := audioQuality(packet.AudioBuffer)
		if peak < 500 {
			t.Fatalf("received silent audio peak=%d", peak)
		}
		if math.Abs(frequency-440) > 120 {
			t.Fatalf("received frequency %.1f Hz, want generated 440 Hz", frequency)
		}
		// A clean sine projects strongly onto its fundamental. This detects
		// severe codec distortion beyond simple packet arrival and pitch checks.
		if purity < 0.65 {
			t.Fatalf("received audio is distorted: 440 Hz purity=%.2f", purity)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for relayed audio")
	}
}
