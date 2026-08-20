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
	// Encoding shares mutable codec state with server-configuration and file
	// playback changes. Keep the client read lock through Encode and Reset so a
	// stereo encoder cannot be replaced or reset while it is in use.
	client.volatile.RLock()
	encoder := client.AudioEncoder
	if client.useStereoEncoder && client.AudioEncoderStereo != nil {
		encoder = client.AudioEncoderStereo
	}
	if encoder == nil {
		client.volatile.RUnlock()
		return nil
	}
	raw, err := encoder.Encode(a, len(a), client.Config.AudioDataBytes)
	if final {
		encoder.Reset()
	}
	var targetID byte
	if target := client.VoiceTarget; target != nil {
		targetID = byte(target.ID)
	}
	client.volatile.RUnlock()
	if err != nil {
		return err
	}
	return client.Conn.WriteAudio(byte(4), targetID, seq, final, raw, nil, nil, nil)
}

// AudioPacket contains incoming audio samples and information.
type AudioPacket struct {
	Client *Client
	Sender *User
	Target *VoiceTarget

	// Sequence is the UDP audio frame timestamp, used by the jitter buffer to
	// reorder packets.
	Sequence int64

	AudioBuffer

	// Terminator marks the final packet in a talk burst. Audio listeners use
	// it to discard ordering state before the sender starts a new burst.
	Terminator bool

	HasPosition      bool
	X, Y, Z          float32
	VolumeAdjustment float32
}
