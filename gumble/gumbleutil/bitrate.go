package gumbleutil // import "git.stormux.org/storm/barnard/gumble/gumbleutil"

import (
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble"
)

var autoBitrate = &Listener{
	Connect: func(e *gumble.ConnectEvent) {
		if e.MaximumBitrate != nil {
			const (
				safety  = 5
				minBytes = 10 // minimum bytes per frame for usable Opus (8 kbps)
			)
			interval := e.Client.Config.AudioInterval
			dataBytes := (*e.MaximumBitrate / (8 * (int(time.Second/interval) + safety))) - 32 - 10

			if dataBytes < minBytes {
				dataBytes = minBytes
			}
			e.Client.Config.AudioDataBytes = dataBytes
		}
	},
}

// AutoBitrate is a gumble.EventListener that automatically sets the client's
// AudioDataBytes to suitable value, based on the server's bitrate.
var AutoBitrate gumble.EventListener

func init() {
	AutoBitrate = autoBitrate
}
