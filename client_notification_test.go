package main

import (
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumble"
)

func TestUserChangeNotification(t *testing.T) {
	current := &gumble.Channel{ID: 1, Name: "Current"}
	other := &gumble.Channel{ID: 2, Name: "Other"}
	self := &gumble.User{Session: 1, Name: "Username", Channel: current}

	tests := []struct {
		name     string
		user     *gumble.User
		previous *gumble.Channel
		change   gumble.UserChangeType
		want     userChangeNotification
		wantOK   bool
	}{
		{
			name:   "connected to current channel",
			user:   &gumble.User{Session: 2, Name: "Guest", Channel: current},
			change: gumble.UserChangeConnected,
			want: userChangeNotification{
				event: "join",
				who:   "Guest",
				what:  "Current",
				line:  "Guest joined Current",
			},
			wantOK: true,
		},
		{
			name:     "disconnected from current channel",
			user:     &gumble.User{Session: 2, Name: "Guest", Channel: current},
			previous: current,
			change:   gumble.UserChangeDisconnected,
			want: userChangeNotification{
				event: "leave",
				who:   "Guest",
				what:  "Current",
				line:  "Guest left Current",
			},
			wantOK: true,
		},
		{
			name:     "moved into current channel",
			user:     &gumble.User{Session: 2, Name: "Guest", Channel: current},
			previous: other,
			change:   gumble.UserChangeChannel,
			want: userChangeNotification{
				event: "join",
				who:   "Guest",
				what:  "Current",
				line:  "Guest joined Current",
			},
			wantOK: true,
		},
		{
			name:     "moved out of current channel",
			user:     &gumble.User{Session: 2, Name: "Guest", Channel: other},
			previous: current,
			change:   gumble.UserChangeChannel,
			want: userChangeNotification{
				event: "leave",
				who:   "Guest",
				what:  "Current",
				line:  "Guest left Current",
			},
			wantOK: true,
		},
		{
			name:   "connected to other channel",
			user:   &gumble.User{Session: 2, Name: "Guest", Channel: other},
			change: gumble.UserChangeConnected,
		},
		{
			name:     "moved between other channels",
			user:     &gumble.User{Session: 2, Name: "Guest", Channel: other},
			previous: &gumble.Channel{ID: 3, Name: "Elsewhere"},
			change:   gumble.UserChangeChannel,
		},
		{
			name:     "self channel move",
			user:     self,
			previous: other,
			change:   gumble.UserChangeChannel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Barnard{
				Client: &gumble.Client{Self: self},
			}
			if tt.previous != nil {
				b.userChannels = map[uint32]*gumble.Channel{tt.user.Session: tt.previous}
			}

			got, ok := b.userChangeNotification(&gumble.UserChangeEvent{
				Client: b.Client,
				Type:   tt.change,
				User:   tt.user,
			})
			if ok != tt.wantOK {
				t.Fatalf("expected ok %v, got %v", tt.wantOK, ok)
			}
			if got != tt.want {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestUpdateUserChannel(t *testing.T) {
	current := &gumble.Channel{ID: 1, Name: "Current"}
	other := &gumble.Channel{ID: 2, Name: "Other"}
	user := &gumble.User{Session: 2, Name: "Guest", Channel: current}
	b := &Barnard{}

	b.updateUserChannel(&gumble.UserChangeEvent{
		Type: gumble.UserChangeConnected,
		User: user,
	})
	if got := b.previousUserChannel(user); got != current {
		t.Fatalf("expected current channel to be remembered, got %#v", got)
	}

	user.Channel = other
	b.updateUserChannel(&gumble.UserChangeEvent{
		Type: gumble.UserChangeChannel,
		User: user,
	})
	if got := b.previousUserChannel(user); got != other {
		t.Fatalf("expected other channel to be remembered, got %#v", got)
	}

	b.updateUserChannel(&gumble.UserChangeEvent{
		Type: gumble.UserChangeDisconnected,
		User: user,
	})
	if got := b.previousUserChannel(user); got != nil {
		t.Fatalf("expected disconnected user channel to be removed, got %#v", got)
	}
}
