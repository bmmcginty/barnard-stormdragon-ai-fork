package main

import (
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumble"
)

// Regression: rebuilding the channel tree ranged protocol-owned maps without
// Client.Do while TCP handlers could add or remove users/channels.
func TestTreeItemBuildReadsMapsUnderClientSnapshot(t *testing.T) {
	root := &gumble.Channel{ID: 0, Users: gumble.Users{}, Children: gumble.Channels{}}
	user := &gumble.User{Session: 1, Name: "user"}
	child := &gumble.Channel{ID: 2, Name: "child", Users: gumble.Users{}, Children: gumble.Channels{}}
	root.Users[user.Session] = user
	root.Children[child.ID] = child
	b := &Barnard{Client: &gumble.Client{Channels: gumble.Channels{0: root}}, MutedChannels: map[uint32]bool{}}
	items := b.TreeItemBuild(TreeItem{Channel: root})
	if len(items) != 2 {
		t.Fatalf("got %d items", len(items))
	}
}
