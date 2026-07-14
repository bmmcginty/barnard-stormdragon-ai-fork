package main

import (
	"fmt"
	"net"
	"strings"
	"time"

	"git.stormux.org/storm/barnard/fileplayback"
	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/gumble/gumbleopenal"
	"git.stormux.org/storm/barnard/gumble/gumbleutil"
	"git.stormux.org/storm/barnard/gumble/opus"
)

func (b *Barnard) start() {
	b.Config.Attach(gumbleutil.AutoBitrate)
	b.Config.Attach(b)
	b.Config.Address = b.Address
	// test Audio
	_, err := gumbleopenal.New(b.Client, b.UserConfig.GetInputDevice(), b.UserConfig.GetOutputDevice(), true)
	if err != nil {
		b.exitWithError(err)
		return
	}
	//connect, not reconnect
	b.connect(false)
}

func (b *Barnard) exitWithError(err error) {
	b.Ui.Close()
	b.exitStatus = 1
	b.exitMessage = err.Error()
}

func (b *Barnard) connect(reconnect bool) bool {
	var err error
	_, err = gumble.DialWithDialer(new(net.Dialer), b.Config, &b.TLSConfig)
	if err != nil {
		if reconnect {
			b.Log(err.Error())
		} else {
			b.exitWithError(err)
		}
		return false
	}

	stream, err := gumbleopenal.New(b.Client, b.UserConfig.GetInputDevice(), b.UserConfig.GetOutputDevice(), false)
	if err != nil {
		b.exitWithError(err)
		return false
	}
	b.Stream = stream
	b.Stream.AttachStream(b.Client)
	b.Stream.SetNoiseProcessor(b.NoiseSuppressor)

	// Initialize stereo encoder for file playback
	b.Client.AudioEncoderStereo = opus.NewStereoEncoder()

	// Initialize file player
	b.FileStreamMutex.Lock()
	b.FileStream = fileplayback.New(b.Client)
	b.FileStream.SetErrorFunc(func(err error) {
		// Disable stereo when file finishes or errors
		b.Client.DisableStereoEncoder()
		b.AddOutputLine(fmt.Sprintf("File playback: %s", err.Error()))
	})
	b.Stream.SetFilePlayer(b.FileStream)
	b.FileStreamMutex.Unlock()

	b.Connected = true
	return true
}

func (b *Barnard) OnConnect(e *gumble.ConnectEvent) {
	b.Client = e.Client

	// Reset muted channels state on connect
	b.MutedChannels = make(map[uint32]bool)
	b.userChannels = make(map[uint32]*gumble.Channel)
	b.RecordingMutex.Lock()
	b.recordingAllowed = nil
	b.recordingStarting = false
	b.RecordingMutex.Unlock()

	b.Ui.SetActive(uiViewInput)
	b.UiTree.Rebuild()
	b.Ui.Refresh()

	for _, u := range b.Client.Users {
		b.UserConfig.UpdateUser(u)
		b.rememberUserChannel(u)
	}

	b.UpdateInputStatus(fmt.Sprintf("[%s]", e.Client.Self.Channel.Name))
	b.AddOutputLine(fmt.Sprintf("Connected to %s", b.Client.Conn.RemoteAddr()))
	wmsg := ""
	if e.WelcomeMessage != nil {
		wmsg = esc(*e.WelcomeMessage)
	}
	b.Notify("connect", "me", wmsg)
	if wmsg != "" {
		b.AddOutputLine(fmt.Sprintf("Welcome message: %s", wmsg))
	}
	b.Ui.Refresh()
}

func (b *Barnard) OnDisconnect(e *gumble.DisconnectEvent) {
	var reason string
	switch e.Type {
	case gumble.DisconnectError:
		reason = "connection error"
	case gumble.DisconnectKicked:
		reason = "kicked"
	case gumble.DisconnectBanned:
		reason = "banned"
	}
	if e.String != "" {
		reason = e.String
	}
	b.stopRecordingForDisconnect()
	b.Notify("disconnect", "me", reason)
	if reason == "" {
		b.AddOutputLine("Disconnected")
	} else {
		b.AddOutputLine("Disconnected: " + reason)
	}
	b.Tx = false
	b.Connected = false
	b.UiTree.Rebuild()
	b.Ui.Refresh()
	go b.reconnectGoroutine()
}

func (b *Barnard) reconnectGoroutine() {
	for {
		res := b.connect(true)
		if res == true {
			break
		}
		time.Sleep(15 * time.Second)
	}
}

func (b *Barnard) Log(s string) {
	b.AddOutputMessage(nil, s)
}

func (b *Barnard) OnTextMessage(e *gumble.TextMessageEvent) {
	var public = false
	for _, c := range e.Channels {
		if c.Name == b.Client.Self.Channel.Name {
			public = true
			break
		}
	}
	if public {
		b.Notify("msg", e.Sender.Name, e.Message)
		b.AddOutputMessage(e.Sender, e.Message)
	} else {
		var sender string
		if e.Sender == nil {
			sender = "Server"
		} else {
			sender = e.Sender.Name
		}
		b.Notify("pm", sender, e.Message)
		b.AddOutputPrivateMessage(e.Sender, b.Client.Self, e.Message)
	}
}

func (b *Barnard) OnUserChange(e *gumble.UserChangeEvent) {
	notification, hasNotification := b.userChangeNotification(e)
	if e.User != nil {
		b.UserConfig.UpdateUser(e.User)

		// Check if user is joining a muted channel
		if e.Type.Has(gumble.UserChangeConnected) || e.Type.Has(gumble.UserChangeChannel) {
			// If the channel is muted, ensure the user is muted
			if b.MutedChannels[e.User.Channel.ID] {
				// Only mute if not already muted
				if !e.User.LocallyMuted {
					b.UserConfig.ToggleMute(e.User)
				}
				if e.User.AudioSource != nil {
					e.User.AudioSource.SetGain(0)
				}
			}
		}
	}

	if e.Type.Has(gumble.UserChangeDisconnected) {
		if e.User == b.selectedUser {
			b.SetSelectedUser(nil)
		}
	}
	if hasNotification {
		b.Notify(notification.event, notification.who, notification.what)
		b.AddOutputLine(notification.line)
	}
	if e.Type.Has(gumble.UserChangeChannel) && e.User == b.Client.Self {
		b.UpdateInputStatus(fmt.Sprintf("[%s]", e.User.Channel.Name))
	}
	if e.Type.Has(gumble.UserChangeRecording) {
		b.HandleRecordingChange(e)
	}
	if e.Type.Has(gumble.UserChangeComment) {
		comment := strings.TrimSpace(esc(e.User.Comment))
		if comment == "" {
			comment = "empty"
		}
		b.AddOutputLine(fmt.Sprintf("User comment for %s: %s", e.User.Name, comment))
	}
	if e.Type.Has(gumble.UserChangeStats) && e.User.Stats != nil {
		b.AddOutputLine(formatUserStats(e.User))
	}
	b.updateUserChannel(e)
	b.RebuildUserChannelTreePreservingSelection()
	b.Ui.Refresh()
}

type userChangeNotification struct {
	event string
	who   string
	what  string
	line  string
}

func (b *Barnard) userChangeNotification(e *gumble.UserChangeEvent) (userChangeNotification, bool) {
	if e == nil || e.User == nil || b.Client == nil || b.Client.Self == nil || b.Client.Self.Channel == nil {
		return userChangeNotification{}, false
	}

	currentChannel := b.Client.Self.Channel
	previousChannel := b.previousUserChannel(e.User)
	userChannel := e.User.Channel

	if e.Type.Has(gumble.UserChangeConnected) {
		return buildUserChangeNotification("join", "joined", e.User, userChannel, currentChannel)
	}
	if e.Type.Has(gumble.UserChangeDisconnected) {
		if previousChannel == nil {
			previousChannel = userChannel
		}
		return buildUserChangeNotification("leave", "left", e.User, previousChannel, currentChannel)
	}
	if e.Type.Has(gumble.UserChangeChannel) && e.User != b.Client.Self {
		if sameChannel(userChannel, currentChannel) && !sameChannel(previousChannel, currentChannel) {
			return buildUserChangeNotification("join", "joined", e.User, userChannel, currentChannel)
		}
		if sameChannel(previousChannel, currentChannel) && !sameChannel(userChannel, currentChannel) {
			return buildUserChangeNotification("leave", "left", e.User, previousChannel, currentChannel)
		}
	}

	return userChangeNotification{}, false
}

func buildUserChangeNotification(event string, verb string, user *gumble.User, eventChannel *gumble.Channel, currentChannel *gumble.Channel) (userChangeNotification, bool) {
	if !sameChannel(eventChannel, currentChannel) {
		return userChangeNotification{}, false
	}
	return userChangeNotification{
		event: event,
		who:   user.Name,
		what:  eventChannel.Name,
		line:  fmt.Sprintf("%s %s %s", user.Name, verb, eventChannel.Name),
	}, true
}

func sameChannel(a *gumble.Channel, b *gumble.Channel) bool {
	return a != nil && b != nil && a.ID == b.ID
}

func (b *Barnard) previousUserChannel(user *gumble.User) *gumble.Channel {
	if b.userChannels == nil || user == nil {
		return nil
	}
	return b.userChannels[user.Session]
}

func (b *Barnard) rememberUserChannel(user *gumble.User) {
	if user == nil || user.Channel == nil {
		return
	}
	if b.userChannels == nil {
		b.userChannels = make(map[uint32]*gumble.Channel)
	}
	b.userChannels[user.Session] = user.Channel
}

func (b *Barnard) updateUserChannel(e *gumble.UserChangeEvent) {
	if e == nil || e.User == nil {
		return
	}
	if e.Type.Has(gumble.UserChangeDisconnected) {
		if b.userChannels != nil {
			delete(b.userChannels, e.User.Session)
		}
		return
	}
	b.rememberUserChannel(e.User)
}

func (b *Barnard) OnChannelChange(e *gumble.ChannelChangeEvent) {
	b.UpdateInputStatus(fmt.Sprintf("[%s]", e.Channel.Name))
	if e.Type.Has(gumble.ChannelChangeDescription) {
		description := strings.TrimSpace(esc(e.Channel.Description))
		if description == "" {
			description = "empty"
		}
		b.AddOutputLine(fmt.Sprintf("Channel description for %s: %s", e.Channel.Name, description))
	}
	if e.Type.Has(gumble.ChannelChangePermission) {
		if permission := e.Channel.Permission(); permission != nil {
			b.AddOutputLine(fmt.Sprintf("Channel permissions for %s: %s", e.Channel.Name, permissionList(*permission)))
		}
	}
	b.RebuildUserChannelTreePreservingSelection()
	b.Ui.Refresh()
}

func formatUserStats(user *gumble.User) string {
	stats := user.Stats
	connected := "unknown"
	if !stats.Connected.IsZero() {
		connected = time.Since(stats.Connected).Round(time.Second).String()
	}
	ip := "unknown"
	if stats.IP != nil {
		ip = stats.IP.String()
	}
	version := formatUserVersion(stats.Version)
	return fmt.Sprintf(
		"User stats for %s: version %s, connected %s, idle %s, bandwidth %d, UDP ping %.1f ms, TCP ping %.1f ms, IP %s, Opus %s",
		user.Name,
		version,
		connected,
		stats.Idle.Round(time.Second),
		stats.Bandwidth,
		stats.UDPPingAverage,
		stats.TCPPingAverage,
		ip,
		onOff(stats.Opus),
	)
}

func formatUserVersion(version gumble.Version) string {
	major, minor, patch := (&version).SemanticVersion()
	semantic := fmt.Sprintf("%d.%d.%d", major, minor, patch)
	parts := []string{}
	if version.Release != "" {
		parts = append(parts, version.Release)
	}
	if version.OS != "" {
		parts = append(parts, version.OS)
	}
	if version.OSVersion != "" {
		parts = append(parts, version.OSVersion)
	}
	if len(parts) == 0 && version.Version == 0 {
		return "unknown"
	}
	if len(parts) == 0 {
		return semantic
	}
	return semantic + " " + strings.Join(parts, " ")
}

func (b *Barnard) OnPermissionDenied(e *gumble.PermissionDeniedEvent) {
	var info string
	switch e.Type {
	case gumble.PermissionDeniedOther:
		info = e.String
	case gumble.PermissionDeniedPermission:
		info = "insufficient permissions"
	case gumble.PermissionDeniedSuperUser:
		info = "cannot modify SuperUser"
	case gumble.PermissionDeniedInvalidChannelName:
		info = "invalid channel name"
	case gumble.PermissionDeniedTextTooLong:
		info = "text too long"
	case gumble.PermissionDeniedTemporaryChannel:
		info = "temporary channel"
	case gumble.PermissionDeniedMissingCertificate:
		info = "missing certificate"
	case gumble.PermissionDeniedInvalidUserName:
		info = "invalid user name"
	case gumble.PermissionDeniedChannelFull:
		info = "channel full"
	case gumble.PermissionDeniedNestingLimit:
		info = "nesting limit"
	}
	b.AddOutputLine(fmt.Sprintf("Permission denied: %s", info))
}

func (b *Barnard) OnUserList(e *gumble.UserListEvent) {
	b.adminUserList = e.UserList
	b.AddOutputLine(fmt.Sprintf("Admin: received %d registered users", len(e.UserList)))
	b.UiAdmin.Rebuild()
	b.Ui.Refresh()
}

func (b *Barnard) OnACL(e *gumble.ACLEvent) {
	b.adminACL = e.ACL
	if e.ACL != nil && e.ACL.Channel != nil {
		b.AddOutputLine(fmt.Sprintf("Admin: received ACLs for %s", e.ACL.Channel.Name))
	}
	b.UiAdmin.Rebuild()
	b.Ui.Refresh()
}

func (b *Barnard) OnBanList(e *gumble.BanListEvent) {
	b.adminBanList = e.BanList
	b.AddOutputLine(fmt.Sprintf("Admin: received %d bans", len(e.BanList)))
	b.UiAdmin.Rebuild()
	b.Ui.Refresh()
}

func (b *Barnard) OnContextActionChange(e *gumble.ContextActionChangeEvent) {
	if e.ContextAction != nil {
		b.AddOutputLine(fmt.Sprintf("Admin: context action updated: %s", e.ContextAction.Name))
	}
	b.UiAdmin.Rebuild()
	b.Ui.Refresh()
}

func (b *Barnard) OnServerConfig(e *gumble.ServerConfigEvent) {
	b.HandleRecordingAllowed(e.RecordingAllowed)
}
