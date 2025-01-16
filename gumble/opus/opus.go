package opus

import (
    "git.2mb.codes/~cmb/barnard/gumble/gumble"
    "github.com/hraban/opus"
)

var Codec gumble.AudioCodec

const (
    ID = 4
    VoiceChannels = 1  // Force mono for voice transmission
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
    e.SetBitrateToMax()
    return &Encoder{
        e,
    }
}

func (*generator) NewDecoder() gumble.AudioDecoder {
    // Create decoder with stereo support
    d, _ := opus.NewDecoder(gumble.AudioSampleRate, gumble.AudioChannels)
    return &Decoder{
        d,
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
    e.Encoder.Reset()
}

// decoder
type Decoder struct {
    *opus.Decoder
}

func (*Decoder) ID() int {
    return ID
}

func (d *Decoder) Decode(data []byte, frameSize int) ([]int16, error) {
    // Allocate buffer for stereo - frameSize is per channel
    pcm := make([]int16, frameSize * gumble.AudioChannels)
    
    // Decode the data
    n, err := d.Decoder.Decode(data, pcm)
    if err != nil {
        return []int16{}, err
    }

    // Return the exact number of samples decoded
    return pcm[:n * gumble.AudioChannels], nil
}

func (d *Decoder) Reset() {
    d.Decoder.Reset()
}
