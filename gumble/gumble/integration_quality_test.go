//go:build integration

package gumble_test

import (
	"math"

	"git.stormux.org/storm/barnard/gumble/gumble"
)

// audioQuality returns the rising-crossing pitch estimate, the fraction of
// RMS energy explained by the 440 Hz fundamental, and the sample peak.
func audioQuality(audio gumble.AudioBuffer) (frequency, purity float64, peak int) {
	if len(audio) < gumble.AudioChannels {
		return 0, 0, 0
	}
	samples := len(audio) / gumble.AudioChannels
	crossings, previous := 0, 0
	var energy, sine, cosine float64
	for i := 0; i < samples; i++ {
		value := int(audio[i*gumble.AudioChannels])
		if value < 0 {
			if -value > peak {
				peak = -value
			}
		} else if value > peak {
			peak = value
		}
		if previous <= 0 && value > 0 {
			crossings++
		}
		previous = value
		x := float64(value)
		phase := 2 * math.Pi * 440 * float64(i) / gumble.AudioSampleRate
		energy += x * x
		sine += x * math.Sin(phase)
		cosine += x * math.Cos(phase)
	}
	frequency = float64(crossings*gumble.AudioSampleRate) / float64(samples)
	if energy != 0 {
		// Projection amplitude divided by RMS, normalized for sine RMS.
		purity = math.Sqrt(2) * math.Hypot(sine, cosine) / math.Sqrt(energy*float64(samples))
	}
	return
}
