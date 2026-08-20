package gumble

import (
	"fmt"
	"time"
)

// Config holds the Mumble configuration used by Client. A single Config should
// not be shared between multiple Client instances.
type Config struct {
	// User name used when authenticating with the server.
	Username string
	// Password used when authenticating with the server. A password is not
	// usually required to connect to a server.
	Password string
	//the address to use
	Address string
	// The initial access tokens to the send to the server. Access tokens can be
	// resent to the server using:
	//  client.Send(config.Tokens)
	Tokens AccessTokens

	// AudioInterval is the interval at which audio packets are sent. Valid
	// values are: 10ms, 20ms, 40ms, and 60ms.
	AudioInterval time.Duration
	// AudioDataBytes is the number of bytes that an audio frame can use.
	AudioDataBytes int
	// IncomingAudioBuffer is the amount of per-speaker audio retained before
	// playback starts, absorbing jitter in incoming UDP packet delivery.
	IncomingAudioBuffer time.Duration

	// DisableUDP forces all audio to use the TCP tunnel instead of UDP.
	DisableUDP bool

	// The event listeners used when client events are triggered.
	Listeners      Listeners
	AudioListeners AudioListeners
	Buffers        int
}

// NewConfig returns a new Config struct with default values set.
func NewConfig() *Config {
	return &Config{
		Buffers:             8,
		AudioInterval:       AudioDefaultInterval,
		AudioDataBytes:      AudioDefaultDataBytes,
		IncomingAudioBuffer: 40 * time.Millisecond,
	}
}

// Validate checks values that are used by the audio ticker and encoder.
func (c *Config) Validate() error {
	switch c.AudioInterval {
	case 10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 60 * time.Millisecond:
	default:
		return fmt.Errorf("gumble: AudioInterval must be 10ms, 20ms, 40ms, or 60ms")
	}
	if c.AudioDataBytes <= 0 {
		return fmt.Errorf("gumble: AudioDataBytes must be positive")
	}
	if c.IncomingAudioBuffer < 0 {
		return fmt.Errorf("gumble: IncomingAudioBuffer must not be negative")
	}
	if c.Buffers <= 0 {
		return fmt.Errorf("gumble: Buffers must be positive")
	}
	return nil
}

// Attach is an alias of c.Listeners.Attach.
func (c *Config) Attach(l EventListener) Detacher {
	return c.Listeners.Attach(l)
}

// AttachAudio is an alias of c.AudioListeners.Attach.
func (c *Config) AttachAudio(l AudioListener) Detacher {
	return c.AudioListeners.Attach(l)
}

// AudioFrameSize returns the appropriate audio frame size, based off of the
// audio interval.
func (c *Config) AudioFrameSize() int {
	return int(c.AudioInterval/AudioDefaultInterval) * AudioDefaultFrameSize
}
