package gumble

import (
	"testing"
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble/MumbleProto"
	"google.golang.org/protobuf/proto"
)

// Regression: an out-of-order user move to an unknown channel used to lock
// volatile twice, permanently deadlocking subsequent protocol handling.
func TestUserStateUnknownChannelDoesNotDeadlock(t *testing.T) {
	c := &Client{Config: NewConfig(), Users: make(Users), Channels: make(Channels)}
	c.Users.create(1)
	id, channel := uint32(1), uint32(99)
	data, _ := proto.Marshal(&MumbleProto.UserState{Session: &id, ChannelId: &channel})
	if err := c.handleUserState(data); err != errInvalidProtobuf {
		t.Fatalf("got %v", err)
	}
	locked := make(chan struct{})
	go func() { c.volatile.Lock(); c.volatile.Unlock(); close(locked) }()
	select {
	case <-locked:
	case <-time.After(time.Second):
		t.Fatal("volatile lock was left locked")
	}
}
