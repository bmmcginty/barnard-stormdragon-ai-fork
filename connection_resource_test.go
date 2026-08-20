package main

import (
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumbleopenal"
)

func TestWithStreamHandlesAbsentConnectionResource(t *testing.T) {
	if (&Barnard{}).withStream(func(*gumbleopenal.Stream) {}) {
		t.Fatal("nil connection stream was treated as available")
	}
}
