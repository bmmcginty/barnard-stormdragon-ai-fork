package main

import (
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumble"
)

func TestAdminActionRejectsRemovedTarget(t *testing.T) {
	user := &gumble.User{Session: 1}
	client := &gumble.Client{Users: gumble.Users{1: user}, Channels: gumble.Channels{}}
	b := &Barnard{Client: client, adminTargetUser: user}
	called := false
	if !b.withValidAdminTargets(func() { called = true }) || !called {
		t.Fatal("current admin target was rejected")
	}
	delete(client.Users, user.Session)
	called = false
	if b.withValidAdminTargets(func() { called = true }) || called {
		t.Fatal("removed admin target was allowed to execute")
	}
}

func TestAdminActionRejectsReplacedChannelTarget(t *testing.T) {
	channel := &gumble.Channel{ID: 2}
	b := &Barnard{Client: &gumble.Client{Channels: gumble.Channels{2: &gumble.Channel{ID: 2}}}, adminTargetChan: channel}
	if b.withValidAdminTargets(func() {}) {
		t.Fatal("replaced channel target was allowed to execute")
	}
}
