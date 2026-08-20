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
	User    *gumble.User
	Channel *gumble.Channel
}

type Barnard struct {
	Config     *gumble.Config
	UserConfig *config.Config
	Hotkeys    *config.Hotkeys
	Client     *gumble.Client

	Address   string
	TLSConfig tls.Config

	Stream    *gumbleopenal.Stream
	Tx        bool
	Connected bool

	Ui              *uiterm.Ui
	UiOutput        uiterm.Textview
	UiInput         uiterm.Textbox
	UiStatus        uiterm.Label
	UiTree          uiterm.Tree
	UiAdmin         uiterm.Tree
	UiInputStatus   uiterm.Label
	SelectedChannel *gumble.Channel
	selectedUser    *gumble.User
	adminTargetUser *gumble.User
	adminTargetChan *gumble.Channel
	adminReturnItem uiterm.TreeItem
	statusText      string
	statusNotice    bool

	notifyChannel chan []string

	exitStatus  int
	exitMessage string

	// Added for channel muting
	MutedChannels map[uint32]bool
	userChannels  map[uint32]*gumble.Channel

	// Added for noise suppression
	NoiseSuppressor *noise.Suppressor

	// Added for file playback
	FileStream      *fileplayback.Player
	FileStreamMutex sync.Mutex

	// Added for recording
	RecordingMutex    sync.Mutex
	Recorder          *recording.Recorder
	recordingStarting bool
	recordingAllowed  *bool

	pendingAdminPrompt *adminPrompt
	adminBanList       gumble.BanList
	adminUserList      gumble.RegisteredUsers
	adminACL           *gumble.ACL
}

func (b *Barnard) StopTransmission() {
	if b.Tx {
		b.Notify("micdown", "me", "")
		b.Tx = false
		b.UpdateGeneralStatus(" Idle ", false)
		b.Stream.StopSource()
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
			if b.selectedUser == treeItem.User {
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
			channelWillBeMuted := !b.MutedChannels[treeItem.Channel.ID]

			// Set all users in channel to the same mute state
			users := makeUsersArray(treeItem.Channel.Users)
			for _, u := range users {
				// Explicitly set user mute state to match channel state
				if channelWillBeMuted && !u.LocallyMuted() {
					b.UserConfig.ToggleMute(u)
				} else if !channelWillBeMuted && u.LocallyMuted() {
					b.UserConfig.ToggleMute(u)
				}

				if source := u.AudioSource(); source != nil {
					if u.LocallyMuted() {
						source.SetGain(0)
					} else {
						source.SetGain(u.Volume())
					}
				}
			}

			// Update channel mute state
			if channelWillBeMuted {
				b.MutedChannels[treeItem.Channel.ID] = true
				// If this is the current channel, stop transmission
				if b.Client.Self.Channel.ID == treeItem.Channel.ID && b.Tx {
					b.StopTransmission()
				}
			} else {
				delete(b.MutedChannels, treeItem.Channel.ID)
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
			b.UserConfig.ToggleMute(treeItem.User)
			if source := treeItem.User.AudioSource(); source != nil {
				if treeItem.User.LocallyMuted() {
					source.SetGain(0)
				} else {
					source.SetGain(treeItem.User.Volume())
				}
			}
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
