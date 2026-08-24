package gumble

import (
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumble/MumbleProto"
	"google.golang.org/protobuf/proto"
)

// Regression: handleChannelState accepted any parent the server named, so a
// channel could be made its own ancestor. Everything that walks the resulting
// Parent/Children graph then recurses until it exhausts memory.
func TestChannelStateRejectsSelfParent(t *testing.T) {
	c := &Client{Config: NewConfig(), Channels: make(Channels)}
	root := c.Channels.create(0)
	child := c.Channels.create(1)
	child.Parent = root
	root.Children[child.ID] = child

	id, parent := child.ID, child.ID
	data, _ := proto.Marshal(&MumbleProto.ChannelState{ChannelId: &id, Parent: &parent})
	if err := c.handleChannelState(data); err != nil {
		t.Fatal(err)
	}

	if child.Parent == child {
		t.Fatal("channel was made its own parent")
	}
	if _, ok := child.Children[child.ID]; ok {
		t.Fatal("channel was made its own child")
	}
	if child.Parent != root {
		t.Fatal("the rejected move should have left the original parent intact")
	}
}

// A channel must not be reparented under one of its own descendants either.
func TestChannelStateRejectsDescendantParent(t *testing.T) {
	c := &Client{Config: NewConfig(), Channels: make(Channels)}
	root := c.Channels.create(0)
	middle := c.Channels.create(1)
	leaf := c.Channels.create(2)
	middle.Parent, root.Children[middle.ID] = root, middle
	leaf.Parent, middle.Children[leaf.ID] = middle, leaf

	id, parent := middle.ID, leaf.ID
	data, _ := proto.Marshal(&MumbleProto.ChannelState{ChannelId: &id, Parent: &parent})
	if err := c.handleChannelState(data); err != nil {
		t.Fatal(err)
	}

	if middle.Parent != root {
		t.Fatal("a cyclic reparent was applied instead of ignored")
	}
	if isChannelDescendant(middle.Parent, middle) {
		t.Fatal("channel graph is cyclic")
	}
}

// A legitimate move must still be applied.
func TestChannelStateAllowsNonCyclicMove(t *testing.T) {
	c := &Client{Config: NewConfig(), Channels: make(Channels)}
	root := c.Channels.create(0)
	a := c.Channels.create(1)
	b := c.Channels.create(2)
	a.Parent, root.Children[a.ID] = root, a
	b.Parent, root.Children[b.ID] = root, b

	id, parent := b.ID, a.ID
	data, _ := proto.Marshal(&MumbleProto.ChannelState{ChannelId: &id, Parent: &parent})
	if err := c.handleChannelState(data); err != nil {
		t.Fatal(err)
	}

	if b.Parent != a {
		t.Fatal("a valid reparent was rejected")
	}
	if a.Children[b.ID] != b {
		t.Fatal("child link missing after a valid reparent")
	}
	if _, ok := root.Children[b.ID]; ok {
		t.Fatal("stale child link left on the old parent")
	}
}
