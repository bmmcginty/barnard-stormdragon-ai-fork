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
	_ = e.SetBitrateToMax()
	return &Encoder{
		e,
	}
}

// NewStereoEncoder creates a stereo encoder for file playback
func NewStereoEncoder() gumble.AudioEncoder {
	// Create stereo encoder for file playback
	e, _ := opus.NewEncoder(gumble.AudioSampleRate, gumble.AudioChannels, opus.AppAudio)
	_ = e.SetBitrateToMax()
	return &Encoder{
		e,
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
}

func (*Encoder) ID() int {
	return ID
}

func (e *Encoder) Encode(pcm []int16, _, maxDataBytes int) ([]byte, error) {
	buf := make([]byte, maxDataBytes)
	n, err := e.Encoder.Encode(pcm, buf)
	if err != nil {
		return []byte{}, err
	}
	return buf[:n], nil
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
	// Allocate buffer for stereo - frameSize is per channel
	pcm := make([]int16, frameSize*gumble.AudioChannels)

	// Decode the data
	n, err := d.Decoder.Decode(data, pcm)
	if err != nil {
		return []int16{}, err
	}

	// Return the exact number of samples decoded
	return pcm[:n*gumble.AudioChannels], nil
}

func (d *Decoder) Reset() {
	decoder, err := opus.NewDecoder(d.sampleRate, d.channels)
	if err == nil {
		d.Decoder = decoder
	}
}
