package main

import (
	"crypto/tls"
	"sync"

	"git.stormux.org/storm/barnard/config"
	"git.stormux.org/storm/barnard/fileplayback"
	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/gumble/gumbleopenal"
	"git.stormux.org/storm/barnard/noise"
	"git.stormux.org/storm/barnard/recording"
	"git.stormux.org/storm/barnard/uiterm"
)

type TreeItem struct {
	User        *gumble.User
	Channel     *gumble.Channel
	display     string
	userSession uint32
	channelID   uint32
	snapshot    bool
}

type Barnard struct {
	Config     *gumble.Config
	UserConfig *config.Config
	Hotkeys    *config.Hotkeys
	Client     *gumble.Client

	Address   string
	TLSConfig tls.Config

	Stream          *gumbleopenal.Stream
	connectionMutex sync.RWMutex
	Tx              bool
	AutoTransmit    bool // auto-start transmission on connect
	Connected       bool
	stateMutex      sync.RWMutex

	Ui                *uiterm.Ui
	UiOutput          uiterm.Textview
	UiInput           uiterm.Textbox
	UiStatus          uiterm.Label
	UiTree            uiterm.Tree
	UiAdmin           uiterm.Tree
	UiInputStatus     uiterm.Label
	SelectedChannel   *gumble.Channel
	selectedUser      *gumble.User
	selectedUserMutex sync.RWMutex
	adminTargetUser   *gumble.User
	adminTargetChan   *gumble.Channel
	adminReturnItem   uiterm.TreeItem
	statusText        string
	statusNotice      bool

	notifyChannel chan []string

	exitStatus  int
	exitMessage string

	// Added for channel muting
	MutedChannels      map[uint32]bool
	MutedChannelsMutex sync.RWMutex
	userChannels       map[uint32]*gumble.Channel

	// Added for noise suppression
	NoiseSuppressor *noise.Suppressor

	// Added for file playback
	FileStream      *fileplayback.Player
	FileStreamMutex sync.Mutex
	// stereoEncoder is reused across connections. Each one holds a little
	// under a megabyte of encoder state, so building a fresh one per
	// reconnect is pure churn; it is reset when file playback ends.
	stereoEncoder gumble.AudioEncoder

	// Added for tone test mode (bypasses all soundcard/OpenAL)
	ToneTest            bool
	ToneTestOutput      string
	toneTestStop        chan struct{}
	toneTestSaver       *AudioFileSaver
	toneTestSaverDetach gumble.Detacher

	// Added for recording
	RecordingMutex    sync.Mutex
	Recorder          *recording.Recorder
	recordingStarting bool
	recordingAllowed  *bool

	pendingAdminPrompt *adminPrompt
	adminBanList       gumble.BanList
	adminUserList      gumble.RegisteredUsers
	adminACL           *gumble.ACL

	reconnectStop     chan struct{}
	reconnectStopOnce sync.Once
	reconnectMutex    sync.Mutex
	reconnecting      bool
}

// cleanupConnectionAudio releases connection-owned audio resources before a
// reconnect replaces them. It is intentionally idempotent for repeated
// disconnect notifications.
func (b *Barnard) cleanupConnectionAudio() {
	// Connection audio operations that use both resources take FileStreamMutex
	// before connectionMutex, so cleanup follows that order as well.
	b.FileStreamMutex.Lock()
	if b.FileStream != nil {
		_ = b.FileStream.Stop()
		b.FileStream = nil
	}
	b.FileStreamMutex.Unlock()
	b.connectionMutex.Lock()
	if b.Stream != nil {
		stream := b.Stream
		b.Stream = nil
		stream.Destroy()
	}
	b.connectionMutex.Unlock()
}

// detachToneTestAudio unsubscribes the saver without closing its output, so a
// reconnect can re-attach the same file. The saver's output is opened
// exclusively and cannot be reopened.
func (b *Barnard) detachToneTestAudio() {
	if b.toneTestSaverDetach != nil {
		b.toneTestSaverDetach.Detach()
		b.toneTestSaverDetach = nil
	}
}

// cleanupToneTestAudio detaches the saver and closes its output. Use it when
// the client is shutting down, not between connections.
func (b *Barnard) cleanupToneTestAudio() {
	b.detachToneTestAudio()
	if b.toneTestSaver != nil {
		b.toneTestSaver.Stop()
		b.toneTestSaver = nil
	}
}

func (b *Barnard) updateUserGain(user *gumble.User) {
	b.withStream(func(stream *gumbleopenal.Stream) {
		stream.UpdateUserGain(user)
	})
}

// withStream keeps a connection-owned stream alive for the complete operation.
// Reconnect cleanup takes the write lock before destroying or replacing it.
func (b *Barnard) withStream(action func(*gumbleopenal.Stream)) bool {
	b.connectionMutex.RLock()
	defer b.connectionMutex.RUnlock()
	if b.Stream == nil {
		return false
	}
	action(b.Stream)
	return true
}

func (b *Barnard) isChannelMuted(channelID uint32) bool {
	b.MutedChannelsMutex.RLock()
	defer b.MutedChannelsMutex.RUnlock()
	return b.MutedChannels[channelID]
}

func (b *Barnard) setChannelMuted(channelID uint32, muted bool) {
	b.MutedChannelsMutex.Lock()
	defer b.MutedChannelsMutex.Unlock()
	if b.MutedChannels == nil {
		b.MutedChannels = make(map[uint32]bool)
	}
	if muted {
		b.MutedChannels[channelID] = true
	} else {
		delete(b.MutedChannels, channelID)
	}
}

func (b *Barnard) selectedUserValue() *gumble.User {
	b.selectedUserMutex.RLock()
	defer b.selectedUserMutex.RUnlock()
	return b.selectedUser
}

func (b *Barnard) setSelectedUserValue(user *gumble.User) {
	b.selectedUserMutex.Lock()
	b.selectedUser = user
	b.selectedUserMutex.Unlock()
}

func (b *Barnard) isTransmitting() bool {
	b.stateMutex.RLock()
	defer b.stateMutex.RUnlock()
	return b.Tx
}

func (b *Barnard) setTransmitting(transmitting bool) {
	b.stateMutex.Lock()
	b.Tx = transmitting
	b.stateMutex.Unlock()
}

func (b *Barnard) isConnected() bool {
	b.stateMutex.RLock()
	defer b.stateMutex.RUnlock()
	return b.Connected
}

func (b *Barnard) setConnected(connected bool) {
	b.stateMutex.Lock()
	b.Connected = connected
	b.stateMutex.Unlock()
}

func (b *Barnard) stopReconnects() {
	b.reconnectStopOnce.Do(func() {
		if b.reconnectStop != nil {
			close(b.reconnectStop)
		}
	})
}

func (b *Barnard) reconnectCanceled() bool {
	if b.reconnectStop == nil {
		return false
	}
	select {
	case <-b.reconnectStop:
		return true
	default:
		return false
	}
}

func (b *Barnard) StopTransmission() {
	if b.isTransmitting() {
		b.Notify("micdown", "me", "")
		b.setTransmitting(false)
		b.UpdateGeneralStatus(" Idle ", false)
		if b.ToneTest {
			// Stop the tone generator.
			if b.toneTestStop != nil {
				close(b.toneTestStop)
				b.toneTestStop = nil
			}
		} else {
			b.withStream(func(stream *gumbleopenal.Stream) { _ = stream.StopSource() })
		}
	}
}

func (b *Barnard) TreeItemCharacter(ui *uiterm.Ui, tree *uiterm.Tree, item uiterm.TreeItem, ch rune) {
}

func (b *Barnard) TreeItemKeyPress(ui *uiterm.Ui, tree *uiterm.Tree, item uiterm.TreeItem, key uiterm.Key) {
	treeItem := item.(TreeItem)
	if key == uiterm.KeyEnter {
		if treeItem.Channel != nil {
			b.Client.Self.Move(treeItem.Channel)
			b.SetSelectedUser(nil)
			b.GotoChat()
		}
		if treeItem.User != nil {
			if b.selectedUserValue() == treeItem.User {
				b.SetSelectedUser(nil)
				b.GotoChat()
			} else {
				b.SetSelectedUser(treeItem.User)
				b.GotoChat()
			}
		}
	}

	// Handle mute toggle
	if treeItem.Channel != nil {
		if key == *b.Hotkeys.MuteToggle {
			// Determine new channel mute state
			channelWillBeMuted := !b.isChannelMuted(treeItem.Channel.ID)

			// Set all users in channel to the same mute state
			users := makeUsersArray(treeItem.Channel.Users)
			for _, u := range users {
				// Explicitly set user mute state to match channel state
				if channelWillBeMuted && !u.LocallyMuted() {
					if err := b.UserConfig.ToggleMute(u); err != nil {
						b.AddOutputLine("Mute: could not save setting: " + err.Error())
					}
				} else if !channelWillBeMuted && u.LocallyMuted() {
					if err := b.UserConfig.ToggleMute(u); err != nil {
						b.AddOutputLine("Mute: could not save setting: " + err.Error())
					}
				}

				b.updateUserGain(u)
			}

			// Update channel mute state
			b.setChannelMuted(treeItem.Channel.ID, channelWillBeMuted)
			if channelWillBeMuted && b.Client.Self.Channel.ID == treeItem.Channel.ID && b.isTransmitting() {
				b.StopTransmission()
			}

			b.RebuildUserChannelTreePreservingSelection()
			b.Ui.Refresh()
		}
		if key == *b.Hotkeys.VolumeDown {
			b.changeVolume(makeUsersArray(treeItem.Channel.Users), -0.1)
		}
		if key == *b.Hotkeys.VolumeUp {
			b.changeVolume(makeUsersArray(treeItem.Channel.Users), 0.1)
		}
		if key == *b.Hotkeys.VolumeReset {
			b.resetVolume(makeUsersArray(treeItem.Channel.Users))
		}
	}

	if treeItem.User != nil {
		if key == *b.Hotkeys.MuteToggle {
			// Toggle mute for single user
			if err := b.UserConfig.ToggleMute(treeItem.User); err != nil {
				b.AddOutputLine("Mute: could not save setting: " + err.Error())
			}
			b.updateUserGain(treeItem.User)
			b.RebuildUserChannelTreePreservingSelection()
			b.Ui.Refresh()
		}
		if key == *b.Hotkeys.VolumeDown {
			b.changeVolume([]*gumble.User{treeItem.User}, -0.1)
		}
		if key == *b.Hotkeys.VolumeUp {
			b.changeVolume([]*gumble.User{treeItem.User}, 0.1)
		}
		if key == *b.Hotkeys.VolumeReset {
			b.resetVolume([]*gumble.User{treeItem.User})
		}
	}
}
