package main

import (
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumble"
)

func TestTreeItemUsesCapturedDisplaySnapshot(t *testing.T) {
	user := &gumble.User{Name: "before"}
	item := TreeItem{User: user, display: "before [100%]"}
	user.Name = "after"
	if got := item.String(); got != "before [100%]" {
		t.Fatalf("tree display read mutable user state: %q", got)
	}
}
