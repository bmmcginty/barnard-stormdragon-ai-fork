package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/gumble/gumbleopenal"
	"git.stormux.org/storm/barnard/uiterm"
	"github.com/kennygrant/sanitize"
	"github.com/nsf/termbox-go"
)

const (
	uiViewLogo        = "logo"
	uiViewTop         = "top"
	uiViewStatus      = "status"
	uiViewInput       = "input"
	uiViewInputStatus = "inputstatus"
	uiViewOutput      = "output"
	uiViewTree        = "tree"
	uiViewAdmin       = "admin"
)

// esc makes server-supplied text safe for a terminal as well as for HTML.
// HTML escaping alone leaves ANSI, OSC, DEL, and bidi/control characters able
// to alter terminal state or obscure the displayed text.
func esc(str string) string {
	clean := strings.Map(func(r rune) rune {
		if r == 0x7f || unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, str)
	return sanitize.HTML(clean)
}

// postUI is the only path network and audio callbacks use to touch terminal
// widgets. Work is dropped during shutdown or queue overload.
func (b *Barnard) postUI(fn func()) {
	if b.Ui != nil {
		b.Ui.Post(fn)
	}
}

func (b *Barnard) Notify(event string, who string, what string) {
	// Notifications are best-effort: a slow external command must not block a
	// UI or network callback. New events are dropped once the bounded queue is full.
	select {
	case b.notifyChannel <- []string{event, who, what}:
	default:
	}
}

func (b *Barnard) SetSelectedUser(user *gumble.User) {
	b.setSelectedUserValue(user)
	if user == nil {
		if len(b.UiInput.Text) > 0 {
		}
		b.UpdateInputStatus(fmt.Sprintf("[%s]", b.Client.Self.Channel.Name))
	} else {
		b.UpdateInputStatus(fmt.Sprintf("[@%s]", user.Name))
	}
}

func (b *Barnard) GetInputStatus() string {
	return b.UiInputStatus.Text
}

func (b *Barnard) UpdateInputStatus(status string) {
	status = truncateInputStatus(status)
	if b.Ui == nil {
		return
	}
	b.Ui.Post(func() {
		b.UiInputStatus.Text = status
		// The initial connection status arrives after Run's first layout. Relayout
		// so the prompt has cells to draw before focus is changed.
		width, height := termbox.Size()
		b.OnUiResize(b.Ui, width, height)
		b.RebuildUserChannelTreePreservingSelection()
		b.Ui.Refresh()
	})
}

// truncateInputStatus limits terminal cells without splitting UTF-8 runes.
func truncateInputStatus(status string) string {
	chars := []rune(status)
	if len(chars) > 20 {
		return string(chars[:17]) + "..." + "]"
	}
	return status
}

func (b *Barnard) AddOutputLine(line string) {
	now := time.Now()
	formatted := fmt.Sprintf("%s [%02d:%02d:%02d]", line, now.Hour(), now.Minute(), now.Second())
	if b.Ui == nil {
		return
	}
	b.Ui.Post(func() { b.UiOutput.AddLine(formatted) })
}

func (b *Barnard) AddOutputMessage(sender *gumble.User, message string) {
	if sender == nil {
		b.AddOutputLine(message)
	} else {
		b.AddOutputLine(fmt.Sprintf("%s: %s", sender.Name, strings.TrimSpace(esc(message))))
	}
}

func (b *Barnard) AddOutputPrivateMessage(source *gumble.User, dest *gumble.User, message string) {
	var sender string
	if source == nil {
		sender = "Server"
	} else {
		sender = source.Name
	}
	b.AddOutputLine(fmt.Sprintf("pm/%s/%s: %s", sender, dest.Name, strings.TrimSpace(esc(message))))
}

func (b *Barnard) OnTimestampToggle(ui *uiterm.Ui, key uiterm.Key) {
	b.UiOutput.ToggleTimestamps()
}

func (b *Barnard) OnNoiseSuppressionToggle(ui *uiterm.Ui, key uiterm.Key) {
	enabled := !b.UserConfig.GetNoiseSuppressionEnabled()
	if err := b.UserConfig.SetNoiseSuppressionEnabled(enabled); err != nil {
		b.AddOutputLine("Noise suppression: could not save setting: " + err.Error())
	}
	b.NoiseSuppressor.SetEnabled(enabled)

	if enabled {
		b.UpdateGeneralStatus("Noise suppression: ON", false)
	} else {
		b.UpdateGeneralStatus("Noise suppression: OFF", false)
	}
}

func (b *Barnard) OnAGCToggle(ui *uiterm.Ui, key uiterm.Key) {
	enabled := b.toggleAGC()

	if enabled {
		b.UpdateGeneralStatus("AGC: ON", false)
	} else {
		b.UpdateGeneralStatus("AGC: OFF", false)
	}
}

// toggleAGC flips the saved AGC preference and applies it to the active
// stream, returning the new state.
func (b *Barnard) toggleAGC() bool {
	enabled := !b.UserConfig.GetAGCEnabled()
	if err := b.UserConfig.SetAGCEnabled(enabled); err != nil {
		b.AddOutputLine("AGC: could not save setting: " + err.Error())
	}
	b.withStream(func(stream *gumbleopenal.Stream) {
		stream.SetAGCEnabled(enabled)
	})
	return enabled
}

func (b *Barnard) UpdateGeneralStatus(text string, notice bool) {
	b.postUI(func() {
		b.statusText = text
		b.statusNotice = notice
		b.renderGeneralStatusNow()
	})
}

func (b *Barnard) renderGeneralStatus() {
	b.postUI(func() { b.renderGeneralStatusNow() })
}

// renderGeneralStatusNow must run on the UI-owning goroutine.
func (b *Barnard) renderGeneralStatusNow() {
	text := b.statusText
	notice := b.statusNotice
	if notice {
		b.UiStatus.Fg = uiterm.ColorWhite | uiterm.AttrBold
		b.UiStatus.Bg = uiterm.ColorRed
	} else {
		b.UiStatus.Fg = uiterm.ColorBlack
		b.UiStatus.Bg = uiterm.ColorWhite
	}
	if b.isRecordingActive() {
		switch strings.TrimSpace(text) {
		case "Idle":
			text = " Idle Rec "
		case "Tx":
			text = " Tx Rec "
		case "File":
			text = " File Rec "
		}
	}
	b.UiStatus.Text = text
	b.Ui.Refresh()
}

func (b *Barnard) OnVoiceToggle(ui *uiterm.Ui, key uiterm.Key) {
	b.setTransmit(ui, 2)
}

func (b *Barnard) CommandLog(ui *uiterm.Ui, cmd string) {
	b.AddOutputLine("command " + cmd)
}

func (b *Barnard) CommandTalk(ui *uiterm.Ui, cmd string) {
	b.setTransmit(ui, 2)
}

func (b *Barnard) CommandMicUp(ui *uiterm.Ui, cmd string) {
	b.setTransmit(ui, 1)
}

func (b *Barnard) CommandMicDown(ui *uiterm.Ui, cmd string) {
	b.setTransmit(ui, 0)
}

func (b *Barnard) CommandNoiseSuppressionToggle(ui *uiterm.Ui, cmd string) {
	enabled := !b.UserConfig.GetNoiseSuppressionEnabled()
	if err := b.UserConfig.SetNoiseSuppressionEnabled(enabled); err != nil {
		b.AddOutputLine("Noise suppression: could not save setting: " + err.Error())
	}
	b.NoiseSuppressor.SetEnabled(enabled)

	if enabled {
		b.AddOutputLine("Noise suppression enabled")
	} else {
		b.AddOutputLine("Noise suppression disabled")
	}
}

func (b *Barnard) CommandAGCToggle(ui *uiterm.Ui, cmd string) {
	if b.toggleAGC() {
		b.AddOutputLine("AGC enabled")
	} else {
		b.AddOutputLine("AGC disabled")
	}
}

func (b *Barnard) CommandPlayFile(ui *uiterm.Ui, cmd string) {
	// cmd contains just the filename part (everything after "/file ")
	filename := strings.TrimSpace(cmd)
	if filename == "" {
		b.AddOutputLine("Usage: /file <filename or URL>")
		return
	}

	// Check if it's a URL
	isURL := strings.HasPrefix(filename, "http://") ||
		strings.HasPrefix(filename, "https://") ||
		strings.HasPrefix(filename, "ftp://") ||
		strings.HasPrefix(filename, "rtmp://")

	if !isURL {
		// Expand ~ to home directory for local files
		if strings.HasPrefix(filename, "~") {
			homeDir := os.Getenv("HOME")
			filename = strings.Replace(filename, "~", homeDir, 1)
		}

		// Check if local file exists
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			b.AddOutputLine(fmt.Sprintf("File not found: %s", filename))
			return
		}
	}

	if !b.isConnected() {
		b.AddOutputLine("Not connected to server")
		return
	}
	if b.ToneTest {
		b.AddOutputLine("File playback is unavailable in tone test mode")
		return
	}

	b.FileStreamMutex.Lock()
	defer b.FileStreamMutex.Unlock()
	if b.FileStream == nil {
		b.AddOutputLine("File playback is unavailable while reconnecting")
		return
	}

	if b.FileStream.IsPlaying() {
		b.AddOutputLine("Already playing a file. Use /stop first.")
		return
	}

	err := b.FileStream.PlayFile(filename)
	if err != nil {
		b.AddOutputLine(fmt.Sprintf("Error playing file: %s", err.Error()))
		return
	}

	// Enable stereo encoder for file playback
	b.Client.EnableStereoEncoder()

	// Auto-start transmission if not already transmitting. FileStreamMutex is
	// held here, before withStream's connection mutex, matching cleanup.
	if !b.isTransmitting() {
		var startErr error
		started := b.withStream(func(stream *gumbleopenal.Stream) {
			startErr = stream.StartSource(b.UserConfig.GetInputDevice())
		})
		if !started {
			startErr = errors.New("audio unavailable while reconnecting")
		}
		if startErr != nil {
			b.AddOutputLine(fmt.Sprintf("Error starting transmission: %s", startErr))
			b.FileStream.Stop()
			b.Client.DisableStereoEncoder()
			return
		}
		b.setTransmitting(true)
		b.UpdateGeneralStatus(" File ", true)
	}

	if isURL {
		b.AddOutputLine(fmt.Sprintf("Streaming: %s (stereo)", filename))
	} else {
		b.AddOutputLine(fmt.Sprintf("Playing: %s (stereo)", filename))
	}
}

func (b *Barnard) CommandStopFile(ui *uiterm.Ui, cmd string) {
	b.FileStreamMutex.Lock()
	defer b.FileStreamMutex.Unlock()

	if b.FileStream == nil || !b.FileStream.IsPlaying() {
		b.AddOutputLine("No file playing")
		return
	}

	err := b.FileStream.Stop()
	if err != nil {
		b.AddOutputLine(fmt.Sprintf("Error stopping file: %s", err.Error()))
		return
	}

	// Disable stereo encoder when file stops
	b.Client.DisableStereoEncoder()

	b.AddOutputLine("File playback stopped")

	// Note: We keep transmission active even after file stops
	// User can manually stop with talk key or it will stop when they're done talking
	b.UpdateGeneralStatus(" Idle ", false)
}

func (b *Barnard) setTransmit(ui *uiterm.Ui, val int) {
	if b.isTransmitting() && val == 1 {
		return
	}
	if !b.isTransmitting() && val == 0 {
		return
	}
	if b.isTransmitting() {
		b.Notify("micdown", "me", "")
		b.setTransmitting(false)
		b.UpdateGeneralStatus(" Idle ", false)
		if b.ToneTest {
			if b.toneTestStop != nil {
				close(b.toneTestStop)
				b.toneTestStop = nil
			}
		} else {
			b.withStream(func(stream *gumbleopenal.Stream) { _ = stream.StopSource() })
		}
	} else if !b.isConnected() {
		b.Notify("error", "me", "no tx while disconnected")
		b.setTransmitting(false)
		b.UpdateGeneralStatus("no tx while disconnected", true)
	} else if b.isChannelMuted(b.Client.Self.Channel.ID) {
		// Check if current channel is muted
		b.Notify("error", "me", "cannot transmit in muted channel")
		b.setTransmitting(false)
		b.UpdateGeneralStatus("cannot transmit in muted channel", true)
	} else {
		b.setTransmitting(true)
		if b.ToneTest {
			b.toneTestStop = make(chan struct{})
			go StartToneGenerator(b.Client, b.toneTestStop)
			b.Notify("micup", "me", "")
			b.UpdateGeneralStatus(" Tx  ", true)
		} else {
			started := b.withStream(func(stream *gumbleopenal.Stream) {
				err := stream.StartSource(b.UserConfig.GetInputDevice())
				if err != nil {
					b.setTransmitting(false)
					if fatalAudioOpenError(err) {
						// A missing capture device cannot recover through normal
						// transmission controls; exit so option 1 reports it on stderr.
						b.exitWithError(fmt.Errorf("audio device initialization failed: %w", err))
						return
					}
					b.Notify("error", "me", err.Error())
					b.UpdateGeneralStatus(err.Error(), true)
					return
				}
				b.Notify("micup", "me", "")
				b.UpdateGeneralStatus(" Tx  ", true)
			})
			if !started {
				b.setTransmitting(false)
				b.UpdateGeneralStatus("audio unavailable while reconnecting", true)
			}
		}
	}
}

func fatalAudioOpenError(err error) bool {
	return errors.Is(err, gumbleopenal.ErrMic) || errors.Is(err, gumbleopenal.ErrInputDevice) || errors.Is(err, gumbleopenal.ErrOutputDevice)
}

func (b *Barnard) OnMicVolumeDown(ui *uiterm.Ui, key uiterm.Key) {
	if b.ToneTest {
		return
	}
	b.withStream(func(stream *gumbleopenal.Stream) {
		stream.SetMicVolume(-0.1, true)
		b.UserConfig.SetMicVolume(stream.GetMicVolume())
		if err := b.UserConfig.SaveConfig(); err != nil {
			b.AddOutputLine("Microphone: could not save volume: " + err.Error())
		}
	})
}

func (b *Barnard) OnMicVolumeUp(ui *uiterm.Ui, key uiterm.Key) {
	if b.ToneTest {
		return
	}
	b.withStream(func(stream *gumbleopenal.Stream) {
		stream.SetMicVolume(0.1, true)
		b.UserConfig.SetMicVolume(stream.GetMicVolume())
		if err := b.UserConfig.SaveConfig(); err != nil {
			b.AddOutputLine("Microphone: could not save volume: " + err.Error())
		}
	})
}

func (b *Barnard) OnQuitPress(ui *uiterm.Ui, key uiterm.Key) {
	b.stopReconnects()
	b.StopRecordingIfActive(true)
	b.Client.Disconnect()
	b.Ui.Close()
}

func (b *Barnard) CommandExit(ui *uiterm.Ui, cmd string) {
	b.stopReconnects()
	b.StopRecordingIfActive(true)
	b.Client.Disconnect()
	b.Ui.Close()
}

func (b *Barnard) CommandStatus(ui *uiterm.Ui, cmd string) {
	if b.isTransmitting() {
		b.Notify("status", "me", "transmitting")
	} else {
		b.Notify("status", "me", "not transmitting")
	}
}

func (b *Barnard) OnClearPress(ui *uiterm.Ui, key uiterm.Key) {
	b.UiOutput.Clear()
}

func (b *Barnard) OnScrollOutputUp(ui *uiterm.Ui, key uiterm.Key) {
	b.UiOutput.ScrollUp()
}

func (b *Barnard) OnScrollOutputDown(ui *uiterm.Ui, key uiterm.Key) {
	b.UiOutput.ScrollDown()
}

func (b *Barnard) OnScrollOutputTop(ui *uiterm.Ui, key uiterm.Key) {
	b.UiOutput.ScrollTop()
}

func (b *Barnard) OnScrollOutputBottom(ui *uiterm.Ui, key uiterm.Key) {
	b.UiOutput.ScrollBottom()
}

func (b *Barnard) OnFocusPress(ui *uiterm.Ui, key uiterm.Key) {
	active := b.Ui.Active()
	if active == uiViewInput {
		b.Ui.SetActive(uiViewTree)
	} else if active == uiViewTree || active == uiViewAdmin {
		b.Ui.SetActive(uiViewInput)
	}
	width, height := termbox.Size()
	b.OnUiResize(ui, width, height)
	ui.Refresh()
}

func (b *Barnard) OnTextInput(ui *uiterm.Ui, textbox *uiterm.Textbox, text string) {
	if b.handleAdminPrompt(text) {
		return
	}
	if text == "" {
		return
	}

	// Check if this is a command (starts with /)
	if strings.HasPrefix(text, "/") {
		// Remove the leading slash and process as command
		cmdText := strings.TrimPrefix(text, "/")
		parts := strings.SplitN(cmdText, " ", 2)
		cmdName := parts[0]
		cmdArgs := ""
		if len(parts) > 1 {
			cmdArgs = parts[1]
		}

		// Handle built-in commands
		switch cmdName {
		case "file":
			b.CommandPlayFile(ui, cmdArgs)
		case "stop":
			b.CommandStopFile(ui, cmdArgs)
		case "exit":
			b.CommandExit(ui, cmdArgs)
		case "status":
			b.CommandStatus(ui, cmdArgs)
		case "noise":
			b.CommandNoiseSuppressionToggle(ui, cmdArgs)
		case "agc":
			b.CommandAGCToggle(ui, cmdArgs)
		case "record":
			b.CommandRecord(ui, cmdArgs)
		case "admin":
			b.CommandAdmin(ui, cmdArgs)
		case "micup":
			b.CommandMicUp(ui, cmdArgs)
		case "micdown":
			b.CommandMicDown(ui, cmdArgs)
		case "toggle", "talk":
			b.CommandTalk(ui, cmdArgs)
		default:
			b.AddOutputLine(fmt.Sprintf("Unknown command: /%s", cmdName))
		}
		return
	}

	// Not a command, send as chat message
	if b.Client != nil && b.Client.Self != nil {
		if selectedUser := b.selectedUserValue(); selectedUser != nil {
			selectedUser.Send(text)
			b.AddOutputPrivateMessage(b.Client.Self, selectedUser, text)
		} else {
			b.Client.Self.Channel.Send(text, false)
			b.AddOutputMessage(b.Client.Self, text)
		}
	}
}

func (b *Barnard) GotoChat() {
	b.OnFocusPress(b.Ui, uiterm.KeyTab)
}

func (b *Barnard) OnUiDoneInitialize(ui *uiterm.Ui) {
	b.start()
}

func (b *Barnard) OnUiInitialize(ui *uiterm.Ui) {
	ui.Add(uiViewLogo, &uiterm.Label{
		Text: "Barnard ",
		Fg:   uiterm.ColorWhite | uiterm.AttrBold,
		Bg:   uiterm.ColorMagenta,
	})

	b.UiStatus = uiterm.Label{
		Text: " Idle ",
		Fg:   uiterm.ColorBlack,
		Bg:   uiterm.ColorWhite,
	}
	b.statusText = b.UiStatus.Text
	b.statusNotice = false
	ui.Add(uiViewStatus, &b.UiStatus)

	b.UiInput = uiterm.Textbox{
		Fg:    uiterm.ColorWhite,
		Bg:    uiterm.ColorBlack,
		Input: b.OnTextInput,
	}
	ui.Add(uiViewInput, &b.UiInput)

	b.UiInputStatus = uiterm.Label{
		Text: "[root]",
		Fg:   uiterm.ColorBlack,
		Bg:   uiterm.ColorWhite,
	}
	ui.Add(uiViewInputStatus, &b.UiInputStatus)

	b.UiOutput = uiterm.Textview{
		Fg: uiterm.ColorWhite,
		Bg: uiterm.ColorBlack,
	}
	ui.Add(uiViewOutput, &b.UiOutput)

	b.UiTree = uiterm.Tree{
		Generator:         b.TreeItemBuild,
		KeyListener:       b.TreeItemKeyPress,
		CharacterListener: b.TreeItemCharacter,
		Fg:                uiterm.ColorWhite,
		Bg:                uiterm.ColorBlack,
	}
	ui.Add(uiViewTree, &b.UiTree)

	b.UiAdmin = uiterm.Tree{
		Generator:         b.AdminItemBuild,
		KeyListener:       b.AdminItemKeyPress,
		CharacterListener: b.AdminItemCharacter,
		Fg:                uiterm.ColorWhite,
		Bg:                uiterm.ColorBlack,
	}
	ui.Add(uiViewAdmin, &b.UiAdmin)

	b.Ui.AddCommandListener(b.CommandMicUp, "micup")
	b.Ui.AddCommandListener(b.CommandMicDown, "micdown")
	b.Ui.AddCommandListener(b.CommandTalk, "toggle")
	b.Ui.AddCommandListener(b.CommandTalk, "talk")
	b.Ui.AddCommandListener(b.CommandExit, "exit")
	b.Ui.AddCommandListener(b.CommandStatus, "status")
	b.Ui.AddCommandListener(b.CommandNoiseSuppressionToggle, "noise")
	b.Ui.AddCommandListener(b.CommandAGCToggle, "agc")
	b.Ui.AddCommandListener(b.CommandPlayFile, "file")
	b.Ui.AddCommandListener(b.CommandStopFile, "stop")
	b.Ui.AddCommandListener(b.CommandRecord, "record")
	b.Ui.AddCommandListener(b.CommandAdmin, "admin")
	b.Ui.AddKeyListener(b.OnFocusPress, b.Hotkeys.SwitchViews)
	b.Ui.AddKeyListener(b.OnAdminMenuPress, b.Hotkeys.AdminMenu)
	b.Ui.AddKeyListener(b.OnVoiceToggle, b.Hotkeys.Talk)
	b.Ui.AddKeyListener(b.OnTimestampToggle, b.Hotkeys.ToggleTimestamps)
	b.Ui.AddKeyListener(b.OnNoiseSuppressionToggle, b.Hotkeys.NoiseSuppressionToggle)
	b.Ui.AddKeyListener(b.OnAGCToggle, b.Hotkeys.AGCToggle)
	b.Ui.AddKeyListener(b.OnRecordingToggle, b.Hotkeys.RecordToggle)
	b.Ui.AddKeyListener(b.OnClearPress, b.Hotkeys.ClearOutput)
	b.Ui.AddKeyListener(b.OnQuitPress, b.Hotkeys.Exit)
	b.Ui.AddKeyListener(b.OnScrollOutputUp, b.Hotkeys.ScrollUp)
	b.Ui.AddKeyListener(b.OnScrollOutputDown, b.Hotkeys.ScrollDown)
	b.Ui.AddKeyListener(b.OnScrollOutputTop, b.Hotkeys.ScrollToTop)
	b.Ui.AddKeyListener(b.OnScrollOutputBottom, b.Hotkeys.ScrollToBottom)
	esc := uiterm.KeyEsc
	b.Ui.AddKeyListener(b.OnAdminEscape, &esc)
	b.Ui.SetActive(uiViewInput)
	b.UiTree.Rebuild()
	b.Ui.Refresh()
}

func (b *Barnard) OnUiResize(ui *uiterm.Ui, width, height int) {
	treeHeight := 0
	outputHeight := 0
	active := b.Ui.Active()
	if active == uiViewTree {
		treeHeight = height - 4
		outputHeight = 0
	} else if active == uiViewAdmin {
		treeHeight = 0
		outputHeight = 0
	} else {
		treeHeight = 0
		outputHeight = height - 4
	}
	ui.SetBounds(uiViewOutput, 0, 1, width, outputHeight+1)
	ui.SetBounds(uiViewTree, 0, 1, width, treeHeight+1)
	if active == uiViewAdmin {
		ui.SetBounds(uiViewAdmin, 0, 1, width, height-3)
	} else {
		ui.SetBounds(uiViewAdmin, 0, 1, width, 1)
	}
	ui.SetBounds(uiViewStatus, 0, height-2, width, height-1)
	ui.SetBounds(uiViewInputStatus, 0, height-1, len(b.GetInputStatus()), height)
	ui.SetBounds(uiViewInput, len(b.GetInputStatus())+1, height-1, width, height)
}
