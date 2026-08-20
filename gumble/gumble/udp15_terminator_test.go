package gumble

import "testing"

type terminatorDecoder struct{ resets int }

func (d *terminatorDecoder) ID() int                             { return audioCodecIDOpus }
func (d *terminatorDecoder) Decode([]byte, int) ([]int16, error) { return nil, nil }
func (d *terminatorDecoder) Reset()                              { d.resets++ }

type terminatorListener struct{ packets chan *AudioPacket }

func (l *terminatorListener) OnAudioStream(e *AudioStreamEvent) {
	go func() { l.packets <- <-e.C }()
}

func TestUDP15EmptyTerminatorResetsAudioListeners(t *testing.T) {
	decoder := &terminatorDecoder{}
	listener := &terminatorListener{packets: make(chan *AudioPacket, 1)}
	config := NewConfig()
	config.AttachAudio(listener)
	user := &User{Session: 1, Name: "speaker", decoder: decoder, audioSequenceValid: true}
	client := &Client{Config: config, Users: Users{user.Session: user}}

	client.dispatchOpus15(1, user.Session, 0, nil, true, 0, nil, 0)

	packet := <-listener.packets
	if !packet.Terminator {
		t.Fatal("empty UDP terminator was not delivered to audio listeners")
	}
	if packet.AudioBuffer != nil {
		t.Fatalf("terminator carried unexpected audio: %v", packet.AudioBuffer)
	}
	if decoder.resets != 1 || user.audioSequenceValid {
		t.Fatalf("terminator did not reset decoder state: resets=%d valid=%v", decoder.resets, user.audioSequenceValid)
	}
}
