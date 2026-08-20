package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/gumble/gumbleopenal"
)

// Regression: HTML escaping left terminal control sequences in server text,
// allowing ANSI/OSC sequences to alter the terminal that rendered it.
// Regression: a capture-device open error left the application alive with no
// usable microphone. These errors are fatal and use the post-TUI stderr path.
func TestFatalAudioOpenError(t *testing.T) {
	for _, err := range []error{gumbleopenal.ErrMic, gumbleopenal.ErrInputDevice, gumbleopenal.ErrOutputDevice, fmt.Errorf("wrapped: %w", gumbleopenal.ErrMic)} {
		if !fatalAudioOpenError(err) {
			t.Fatalf("%v was not fatal", err)
		}
	}
	if fatalAudioOpenError(gumbleopenal.ErrState) {
		t.Fatal("state error should remain recoverable")
	}
}

func TestEscRemovesTerminalControlSequences(t *testing.T) {
	got := esc("name\x1b]0;spoof\a\x7f\u202e")
	if got != "name]0;spoof" {
		t.Fatalf("unsafe terminal text %q", got)
	}
}

type testReadCloser struct{ io.Reader }

func (testReadCloser) Close() error { return nil }

// Regression: an EOF from the FIFO was ignored and caused an unbounded busy
// loop. The reader must deliver a final command then close its output.
func TestReadFIFOStopsOnEOF(t *testing.T) {
	out := make(chan string)
	go readFIFO(testReadCloser{strings.NewReader("command\n")}, out)
	if got := <-out; got != "command" {
		t.Fatalf("got %q", got)
	}
	if _, ok := <-out; ok {
		t.Fatal("FIFO output remained open after EOF")
	}
}

// Regression: sequential substitutions re-expanded placeholders embedded in
// server-provided fields, and a slow notifier blocked callback goroutines.
func TestNotificationExpansionIsSinglePassAndNotifyDoesNotBlock(t *testing.T) {
	got := expandNotification("%event %what", []string{"event", "who", "%event"})
	if got != "event %event" {
		t.Fatalf("unexpected expansion %q", got)
	}
	b := &Barnard{notifyChannel: make(chan []string, 1)}
	b.Notify("one", "", "")
	done := make(chan struct{})
	go func() { b.Notify("two", "", ""); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Notify blocked on a full queue")
	}
}

func TestAudioIntervalDuration(t *testing.T) {
	for _, milliseconds := range []int{10, 20, 40, 60} {
		got, err := audioIntervalDuration(milliseconds)
		if err != nil {
			t.Errorf("audioIntervalDuration(%d): %v", milliseconds, err)
			continue
		}
		if got != time.Duration(milliseconds)*time.Millisecond {
			t.Errorf("audioIntervalDuration(%d) = %v", milliseconds, got)
		}
	}
	if _, err := audioIntervalDuration(30); err == nil {
		t.Fatal("audioIntervalDuration accepted unsupported duration")
	}
}

func TestJitterBufferDuration(t *testing.T) {
	for _, milliseconds := range []int{0, 20, 40, 60} {
		got, err := jitterBufferDuration(milliseconds)
		if err != nil {
			t.Errorf("jitterBufferDuration(%d): %v", milliseconds, err)
			continue
		}
		if got != time.Duration(milliseconds)*time.Millisecond {
			t.Errorf("jitterBufferDuration(%d) = %v", milliseconds, got)
		}
	}
	if _, err := jitterBufferDuration(10); err == nil {
		t.Fatal("jitterBufferDuration accepted unsupported duration")
	}
}

func TestServerAddressDefaultsPortWithoutBreakingIPv6(t *testing.T) {
	for input, want := range map[string]string{
		"server":          "server:64738",
		"server:64739":    "server:64739",
		"::1":             "[::1]:64738",
		"[2001:db8::1]":   "[2001:db8::1]:64738",
		"[2001:db8::1]:9": "[2001:db8::1]:9",
	} {
		if got := serverAddress(input); got != want {
			t.Errorf("serverAddress(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestConcurrentConnectionStateAccess(t *testing.T) {
	b := &Barnard{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(value bool) {
			defer wg.Done()
			b.setConnected(value)
			b.setTransmitting(value)
			_ = b.isConnected()
			_ = b.isTransmitting()
		}(i%2 == 0)
	}
	wg.Wait()
}

func TestConcurrentSelectedUserAccess(t *testing.T) {
	b := &Barnard{}
	user := &gumble.User{Session: 1}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(user *gumble.User) {
			defer wg.Done()
			b.setSelectedUserValue(user)
			_ = b.selectedUserValue()
		}(user)
	}
	wg.Wait()
}

func TestConcurrentMutedChannelAccess(t *testing.T) {
	b := &Barnard{MutedChannels: make(map[uint32]bool)}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b.setChannelMuted(uint32(i%3), i%2 == 0)
			_ = b.isChannelMuted(uint32((i + 1) % 3))
		}(i)
	}
	wg.Wait()
}

func TestPublicTextMessageTargetsChannelIDsAndTrees(t *testing.T) {
	root := &gumble.Channel{ID: 1, Name: "Root"}
	current := &gumble.Channel{ID: 2, Name: "Room", Parent: root}
	b := &Barnard{Client: &gumble.Client{Self: &gumble.User{Channel: current}}}

	if !b.isPublicTextMessage(&gumble.TextMessageEvent{TextMessage: gumble.TextMessage{Trees: []*gumble.Channel{root}}}) {
		t.Fatal("recursive message to an ancestor was not public")
	}
	other := &gumble.Channel{ID: 3, Name: "Room"}
	if b.isPublicTextMessage(&gumble.TextMessageEvent{TextMessage: gumble.TextMessage{Channels: []*gumble.Channel{other}}}) {
		t.Fatal("message to a different channel with the same name was public")
	}
}

func TestPublicServerMessageDoesNotPanic(t *testing.T) {
	channel := &gumble.Channel{ID: 1, Name: "Current"}
	b := &Barnard{
		Client:        &gumble.Client{Self: &gumble.User{Channel: channel}},
		notifyChannel: make(chan []string, 1),
	}

	b.OnTextMessage(&gumble.TextMessageEvent{
		Client: b.Client,
		TextMessage: gumble.TextMessage{
			Channels: []*gumble.Channel{channel},
			Message:  "server announcement",
		},
	})

	got := <-b.notifyChannel
	want := []string{"msg", "Server", "server announcement"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("notification = %#v, want %#v", got, want)
	}
}

// Regression: reconnect replaced Stream without destroying the old capture
// and renderer resources. Cleanup must be safe for repeated disconnects.
// Regression: user and channel names in the navigation tree bypassed message
// escaping and could still carry terminal control characters.
// Regression: status truncation used byte indexes and could create invalid
// UTF-8 when a non-ASCII user or channel name exceeded the display limit.
func TestTruncateInputStatusPreservesUTF8(t *testing.T) {
	got := truncateInputStatus(strings.Repeat("é", 21))
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 21 {
		t.Fatalf("invalid truncation %q", got)
	}
}

func TestTreeItemSanitizesServerNames(t *testing.T) {
	item := TreeItem{Channel: &gumble.Channel{Name: "\x1b[2Jroom"}}
	if got := item.String(); got != "#[2Jroom" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanupConnectionAudioIsIdempotent(t *testing.T) {
	b := &Barnard{}
	b.cleanupConnectionAudio()
	b.cleanupConnectionAudio()
}

// Tone test mode intentionally does not create an OpenAL stream. Tree
// controls must therefore keep local mute state without trying to update one.
func TestReconnectCancellationIsSafeBeforeStartup(t *testing.T) {
	b := &Barnard{}
	b.stopReconnects()
	if b.reconnectCanceled() {
		t.Fatal("nil reconnect channel should not report cancellation")
	}
}

func TestReconnectCancellationStopsWaiters(t *testing.T) {
	b := &Barnard{reconnectStop: make(chan struct{})}
	b.stopReconnects()
	if !b.reconnectCanceled() {
		t.Fatal("expected reconnect cancellation")
	}
	b.stopReconnects() // repeated shutdown must not panic
}

func TestUpdateUserGainAllowsToneTestWithoutStream(t *testing.T) {
	(&Barnard{ToneTest: true}).updateUserGain(&gumble.User{})
}

func TestToneTestRejectsFilePlayback(t *testing.T) {
	(&Barnard{ToneTest: true, Connected: true}).CommandPlayFile(nil, "https://example.invalid/audio")
}

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
