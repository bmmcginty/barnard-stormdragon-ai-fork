package gumble

import (
	"time"
)

const (
	// AudioSampleRate is the audio sample rate (in hertz) for incoming and
	// outgoing audio.
	AudioSampleRate = 48000

	// AudioDefaultInterval is the default interval that audio packets are sent
	// at.
	AudioDefaultInterval = 10 * time.Millisecond

	// AudioMonoChannels is the number of channels used for voice transmission
	AudioMonoChannels = 1

	// AudioChannels is the number of channels used for playback
	AudioChannels = 2

	// AudioDefaultFrameSize is the number of audio frames that should be sent in
	// a 10ms window (mono samples)
	AudioDefaultFrameSize = AudioSampleRate / 100

	// AudioMaximumFrameSize is the maximum audio frame size from another user
	// that will be processed (accounting for stereo)
	AudioMaximumFrameSize = (AudioSampleRate / 1000 * 60) * AudioChannels

	// AudioDefaultDataBytes is the default number of bytes that an audio frame
	// can use.
	AudioDefaultDataBytes = 40
)

// AudioListener is the interface that must be implemented by types wishing to
// receive incoming audio data from the server.
//
// OnAudioStream is called when an audio stream for a user starts. It is the
// implementer's responsibility to continuously process AudioStreamEvent.C
// until it is closed.
type AudioListener interface {
	OnAudioStream(e *AudioStreamEvent)
}

// AudioStreamEvent is event that is passed to AudioListener.OnAudioStream.
type AudioStreamEvent struct {
	Client *Client
	User   *User
	C      <-chan *AudioPacket
}

// AudioBuffer is a slice of PCM audio samples.
type AudioBuffer []int16

func (a AudioBuffer) writeAudio(client *Client, seq int64, final bool) error {
	// Choose encoder based on whether buffer size indicates stereo or mono
	encoder := client.AudioEncoder
	frameSize := client.Config.AudioFrameSize()
	if len(a) == frameSize*AudioChannels && client.AudioEncoderStereo != nil {
		encoder = client.AudioEncoderStereo
	} else if client.IsStereoEncoderEnabled() && client.AudioEncoderStereo != nil {
		encoder = client.AudioEncoderStereo
	}
	if encoder == nil {
		return nil
	}
	dataBytes := client.Config.AudioDataBytes
	raw, err := encoder.Encode(a, len(a), dataBytes)
	if final {
		defer encoder.Reset()
	}
	if err != nil {
		return err
	}

	var targetID byte
	if target := client.VoiceTarget; target != nil {
		targetID = byte(target.ID)
	}
	return client.Conn.WriteAudio(byte(4), targetID, seq, final, raw, nil, nil, nil)
}

// AudioPacket contains incoming audio samples and information.
type AudioPacket struct {
	Client *Client
	Sender *User
	Target *VoiceTarget

	AudioBuffer

	HasPosition bool
	X, Y, Z     float32
}
