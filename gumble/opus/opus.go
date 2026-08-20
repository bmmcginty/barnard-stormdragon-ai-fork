package opus

import (
	"github.com/hraban/opus"

	"git.stormux.org/storm/barnard/gumble/gumble"
)

var Codec gumble.AudioCodec

const (
	ID            = 4
	VoiceChannels = 1 // Force mono for voice transmission
)

func init() {
	Codec = &generator{}
	gumble.RegisterAudioCodec(4, Codec)
}

// generator
type generator struct {
}

func (*generator) ID() int {
	return ID
}

func (*generator) NewEncoder() gumble.AudioEncoder {
	// Force mono for voice transmission
	e, _ := opus.NewEncoder(gumble.AudioSampleRate, VoiceChannels, opus.AppVoIP)
	return &Encoder{
		Encoder:  e,
		channels: VoiceChannels,
	}
}

// NewStereoEncoder creates a stereo encoder for file playback
func NewStereoEncoder() gumble.AudioEncoder {
	// Create stereo encoder for file playback
	e, _ := opus.NewEncoder(gumble.AudioSampleRate, gumble.AudioChannels, opus.AppAudio)
	return &Encoder{
		Encoder:  e,
		channels: gumble.AudioChannels,
	}
}

func (*generator) NewDecoder() gumble.AudioDecoder {
	// Create decoder with stereo support
	d, _ := opus.NewDecoder(gumble.AudioSampleRate, gumble.AudioChannels)
	return &Decoder{
		Decoder:    d,
		sampleRate: gumble.AudioSampleRate,
		channels:   gumble.AudioChannels,
	}
}

// encoder
type Encoder struct {
	*opus.Encoder
	channels int
}

func (*Encoder) ID() int {
	return ID
}

func (e *Encoder) Encode(pcm []int16, frameSamples, maxDataBytes int) ([]byte, error) {
	bitrate := encoderBitrate(maxDataBytes, frameSamples, e.channels)
	if bitrate < 8000 {
		bitrate = 8000 // Opus minimum viable bitrate for voice
	}
	_ = e.Encoder.SetBitrate(bitrate)

	buf := make([]byte, maxDataBytes)
	n, err := e.Encoder.Encode(pcm, buf)
	if err != nil {
		return []byte{}, err
	}
	return buf[:n], nil
}

// encoderBitrate converts a per-frame packet budget to bits per second.
func encoderBitrate(maxDataBytes, frameSamples, channels int) int {
	if frameSamples <= 0 || channels <= 0 {
		return 8000
	}
	return maxDataBytes * 8 * gumble.AudioSampleRate * channels / frameSamples
}

func (e *Encoder) Reset() {
	_ = e.Encoder.Reset()
}

// decoder
type Decoder struct {
	*opus.Decoder
	sampleRate int
	channels   int
}

func (*Decoder) ID() int {
	return ID
}

func (d *Decoder) Decode(data []byte, frameSize int) ([]int16, error) {
	// frameSize is the maximum number of PCM samples (all channels
	// combined). The underlying Opus decoder output is interleaved
	// stereo, so the buffer holds left+right pairs.
	pcm := make([]int16, frameSize)

	// Decode the data. If data is nil/empty, the decoder performs
	// Packet Loss Concealment and produces a concealed frame.
	n, err := d.Decoder.Decode(data, pcm)
	if err != nil {
		return []int16{}, err
	}

	// n is the number of samples per channel; stereo interleaved
	// output means total samples = n * channels.
	return pcm[:n*d.channels], nil
}

func (d *Decoder) Reset() {
	decoder, err := opus.NewDecoder(d.sampleRate, d.channels)
	if err == nil {
		d.Decoder = decoder
	}
}
