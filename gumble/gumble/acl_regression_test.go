package gumble

import (
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumble/MumbleProto"
	"google.golang.org/protobuf/proto"
)

// Regression: an ACL group without its optional name dereferenced nil in the
// TCP handler, allowing malformed server data to crash the client.
func TestACLRejectsGroupWithoutName(t *testing.T) {
	id := uint32(1)
	packet := &MumbleProto.ACL{ChannelId: &id, Groups: []*MumbleProto.ACL_ChanGroup{{}}}
	data, err := proto.MarshalOptions{AllowPartial: true}.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{Config: NewConfig(), Channels: make(Channels)}
	c.Channels.create(id)
	if err := c.handleACL(data); err == nil {
		t.Fatal("accepted malformed ACL group")
	}
}
