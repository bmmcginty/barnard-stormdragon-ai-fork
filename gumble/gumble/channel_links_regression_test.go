package gumble

import (
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumble/MumbleProto"
	"google.golang.org/protobuf/proto"
)

// Regression: a full channel-link update used to leave the removed peer's
// reverse link behind, making the client report a link that no longer exists.
func TestChannelStateFullLinksRemovesReverseLinks(t *testing.T) {
	c := &Client{Config: NewConfig(), Channels: make(Channels)}
	a, b, replacement := c.Channels.create(1), c.Channels.create(2), c.Channels.create(3)
	a.Links[b.ID], b.Links[a.ID] = b, a
	id := a.ID
	data, _ := proto.Marshal(&MumbleProto.ChannelState{ChannelId: &id, Links: []uint32{replacement.ID}})
	if err := c.handleChannelState(data); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Links[a.ID]; ok {
		t.Fatal("stale reciprocal link remains")
	}
	if replacement.Links[a.ID] != a {
		t.Fatal("replacement reciprocal link missing")
	}
}
