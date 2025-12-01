package main

import (
    "fmt"
    "os"
    "os/exec"
    "strings"
    "time"

    "git.stormux.org/storm/barnard/gumble/gumble"
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
)

func Beep() {
    cmd := exec.Command("beep")
    cmdout, err := cmd.Output()
    if err != nil {
        panic(err)
    }
    if cmdout != nil {
    }
}

func esc(str string) string {
    return sanitize.HTML(str)
}

func (b *Barnard) Notify(event string, who string, what string) {
    b.notifyChannel <- []string{event, who, what}
}

func (b *Barnard) Beep() {
    Beep()
}

func (b *Barnard) SetSelectedUser(user *gumble.User) {
    b.selectedUser = user
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
    if len(status) > 20 {
        status = status[:17] + "..." + "]"
    }
    b.UiInputStatus.Text = status
    b.UiTree.Rebuild()
    b.Ui.Refresh()
}

func (b *Barnard) AddOutputLine(line string) {
    now := time.Now()
    b.UiOutput.AddLine(fmt.Sprintf("[%02d:%02d:%02d] %s", now.Hour(), now.Minute(), now.Second(), line))
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
    b.UserConfig.SetNoiseSuppressionEnabled(enabled)
    b.NoiseSuppressor.SetEnabled(enabled)

    if enabled {
        b.UpdateGeneralStatus("Noise suppression: ON", false)
    } else {
        b.UpdateGeneralStatus("Noise suppression: OFF", false)
    }
}

func (b *Barnard) OnVoiceEffectCycle(ui *uiterm.Ui, key uiterm.Key) {
    effect := b.VoiceEffects.CycleEffect()
    b.UserConfig.SetVoiceEffect(int(effect))
    b.UpdateGeneralStatus(fmt.Sprintf("Voice effect: %s", effect.String()), false)
}


func (b *Barnard) UpdateGeneralStatus(text string, notice bool) {
    if notice {
        b.UiStatus.Fg = uiterm.ColorWhite | uiterm.AttrBold
        b.UiStatus.Bg = uiterm.ColorRed
    } else {
        b.UiStatus.Fg = uiterm.ColorBlack
        b.UiStatus.Bg = uiterm.ColorWhite
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
    b.UserConfig.SetNoiseSuppressionEnabled(enabled)
    b.NoiseSuppressor.SetEnabled(enabled)

    if enabled {
        b.AddOutputLine("Noise suppression enabled")
    } else {
        b.AddOutputLine("Noise suppression disabled")
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

    if !b.Connected {
        b.AddOutputLine("Not connected to server")
        return
    }

    b.FileStreamMutex.Lock()
    defer b.FileStreamMutex.Unlock()

    if b.FileStream != nil && b.FileStream.IsPlaying() {
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

    // Auto-start transmission if not already transmitting
    if !b.Tx {
        err := b.Stream.StartSource(b.UserConfig.GetInputDevice())
        if err != nil {
            b.AddOutputLine(fmt.Sprintf("Error starting transmission: %s", err.Error()))
            b.FileStream.Stop()
            b.Client.DisableStereoEncoder()
            return
        }
        b.Tx = true
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
    if b.Tx && val == 1 {
        return
    }
    if b.Tx == false && val == 0 {
        return
    }
    if b.Tx {
        b.Notify("micdown", "me", "")
        b.Tx = false
        b.UpdateGeneralStatus(" Idle ", false)
        b.Stream.StopSource()
    } else if b.Connected == false {
        b.Notify("error", "me", "no tx while disconnected")
        b.Tx = false
        b.UpdateGeneralStatus("no tx while disconnected", true)
    } else if b.MutedChannels[b.Client.Self.Channel.ID] {
        // Check if current channel is muted
        b.Notify("error", "me", "cannot transmit in muted channel")
        b.Tx = false
        b.UpdateGeneralStatus("cannot transmit in muted channel", true)
    } else {
        b.Tx = true
        err := b.Stream.StartSource(b.UserConfig.GetInputDevice())
        if err != nil {
            b.Notify("error", "me", err.Error())
            b.UpdateGeneralStatus(err.Error(), true)
        } else {
            b.Notify("micup", "me", "")
            b.UpdateGeneralStatus(" Tx  ", true)
        }
    }
}

func (b *Barnard) OnMicVolumeDown(ui *uiterm.Ui, key uiterm.Key) {
    b.Stream.SetMicVolume(-0.1, true)
    b.UserConfig.SetMicVolume(b.Stream.GetMicVolume())
}

func (b *Barnard) OnMicVolumeUp(ui *uiterm.Ui, key uiterm.Key) {
    b.Stream.SetMicVolume(0.1, true)
    b.UserConfig.SetMicVolume(b.Stream.GetMicVolume())
}

func (b *Barnard) OnQuitPress(ui *uiterm.Ui, key uiterm.Key) {
    b.Client.Disconnect()
    b.Ui.Close()
}

func (b *Barnard) CommandExit(ui *uiterm.Ui, cmd string) {
    b.Client.Disconnect()
    b.Ui.Close()
}

func (b *Barnard) CommandStatus(ui *uiterm.Ui, cmd string) {
    if b.Tx {
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
    } else if active == uiViewTree {
        b.Ui.SetActive(uiViewInput)
    }
    width, height := termbox.Size()
    b.OnUiResize(ui, width, height)
    ui.Refresh()
}

func (b *Barnard) OnTextInput(ui *uiterm.Ui, textbox *uiterm.Textbox, text string) {
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
        if b.selectedUser != nil {
            b.selectedUser.Send(text)
            b.AddOutputPrivateMessage(b.Client.Self, b.selectedUser, text)
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
    ui.Add(uiViewStatus, &b.UiStatus)

    b.UiInput = uiterm.Textbox{
        Fg:    uiterm.ColorWhite,
        Bg:    uiterm.ColorBlack,
        Input: b.OnTextInput,
    }
    ui.Add(uiViewInput, &b.UiInput)

    b.UiInputStatus = uiterm.Label{
        Fg: uiterm.ColorBlack,
        Bg: uiterm.ColorWhite,
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
        Fg:               uiterm.ColorWhite,
        Bg:               uiterm.ColorBlack,
    }
    ui.Add(uiViewTree, &b.UiTree)

    b.Ui.AddCommandListener(b.CommandMicUp, "micup")
    b.Ui.AddCommandListener(b.CommandMicDown, "micdown")
    b.Ui.AddCommandListener(b.CommandTalk, "toggle")
    b.Ui.AddCommandListener(b.CommandTalk, "talk")
    b.Ui.AddCommandListener(b.CommandExit, "exit")
    b.Ui.AddCommandListener(b.CommandStatus, "status")
    b.Ui.AddCommandListener(b.CommandNoiseSuppressionToggle, "noise")
    b.Ui.AddCommandListener(b.CommandPlayFile, "file")
    b.Ui.AddCommandListener(b.CommandStopFile, "stop")
    b.Ui.AddKeyListener(b.OnFocusPress, b.Hotkeys.SwitchViews)
    b.Ui.AddKeyListener(b.OnVoiceToggle, b.Hotkeys.Talk)
    b.Ui.AddKeyListener(b.OnTimestampToggle, b.Hotkeys.ToggleTimestamps)
    b.Ui.AddKeyListener(b.OnNoiseSuppressionToggle, b.Hotkeys.NoiseSuppressionToggle)
    b.Ui.AddKeyListener(b.OnVoiceEffectCycle, b.Hotkeys.CycleVoiceEffect)
    b.Ui.AddKeyListener(b.OnQuitPress, b.Hotkeys.Exit)
    b.Ui.AddKeyListener(b.OnScrollOutputUp, b.Hotkeys.ScrollUp)
    b.Ui.AddKeyListener(b.OnScrollOutputDown, b.Hotkeys.ScrollDown)
    b.Ui.AddKeyListener(b.OnScrollOutputTop, b.Hotkeys.ScrollToTop)
    b.Ui.AddKeyListener(b.OnScrollOutputBottom, b.Hotkeys.ScrollToBottom)
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
    } else {
        treeHeight = 0
        outputHeight = height - 4
    }
    ui.SetBounds(uiViewOutput, 0, 1, width, outputHeight+1)
    ui.SetBounds(uiViewTree, 0, 1, width, treeHeight+1)
    ui.SetBounds(uiViewStatus, 0, height-2, width, height-1)
    ui.SetBounds(uiViewInputStatus, 0, height-1, len(b.GetInputStatus()), height)
    ui.SetBounds(uiViewInput, len(b.GetInputStatus())+1, height-1, width, height)
}
