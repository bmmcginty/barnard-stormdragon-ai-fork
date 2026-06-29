package main

import (
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/uiterm"
)

func TestParseToggleState(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		current bool
		want    bool
		wantOK  bool
	}{
		{name: "toggle true", text: "toggle", current: true, want: false, wantOK: true},
		{name: "empty toggles", text: "", current: false, want: true, wantOK: true},
		{name: "on", text: "on", current: false, want: true, wantOK: true},
		{name: "off", text: "off", current: true, want: false, wantOK: true},
		{name: "bad", text: "maybe", current: true, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseToggleState(tt.text, tt.current)
			if ok != tt.wantOK {
				t.Fatalf("expected ok %v, got %v", tt.wantOK, ok)
			}
			if ok && got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestParsePermission(t *testing.T) {
	tests := []struct {
		text string
		want gumble.Permission
	}{
		{text: "write", want: gumble.PermissionWrite},
		{text: "mute_deafen", want: gumble.PermissionMuteDeafen},
		{text: "kick", want: gumble.PermissionKick},
		{text: "register_self", want: gumble.PermissionRegisterSelf},
	}
	for _, tt := range tests {
		got, ok := parsePermission(tt.text)
		if !ok {
			t.Fatalf("expected %s to parse", tt.text)
		}
		if got != tt.want {
			t.Fatalf("expected %v, got %v", tt.want, got)
		}
	}
	if _, ok := parsePermission("bogus"); ok {
		t.Fatal("expected bogus permission to fail")
	}
}

func TestPermissionList(t *testing.T) {
	got := permissionList(gumble.PermissionWrite | gumble.PermissionBan)
	if got != "write,ban" {
		t.Fatalf("expected write,ban, got %q", got)
	}
}

func TestAdminEscapeInputs(t *testing.T) {
	if !isAdminEscapeKey(uiterm.KeyEsc) {
		t.Fatal("expected escape key to close admin menu")
	}
	if !isAdminEscapeKey(uiterm.KeyAltEsc) {
		t.Fatal("expected alt escape key to close admin menu")
	}
	if !isAdminEscapeKey(uiterm.KeyAltArrowUp) {
		t.Fatal("expected escape-prefixed up arrow to close admin menu")
	}
	if !isAdminEscapeKey(uiterm.KeyAltArrowDown) {
		t.Fatal("expected escape-prefixed down arrow to close admin menu")
	}
	if isAdminEscapeKey(uiterm.KeyEnter) {
		t.Fatal("expected enter key not to close admin menu")
	}
	if !isAdminEscapeCharacter(rune(uiterm.KeyEsc)) {
		t.Fatal("expected escape character to close admin menu")
	}
	if isAdminEscapeCharacter('x') {
		t.Fatal("expected non-escape character not to close admin menu")
	}
}
