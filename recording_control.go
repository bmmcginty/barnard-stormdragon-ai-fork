package main

import (
	"fmt"
	"strings"
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/recording"
	"git.stormux.org/storm/barnard/uiterm"
)

func (b *Barnard) OnRecordingToggle(ui *uiterm.Ui, key uiterm.Key) {
	b.ToggleRecording()
}

func (b *Barnard) CommandRecord(ui *uiterm.Ui, cmd string) {
	action := strings.ToLower(strings.TrimSpace(cmd))
	switch action {
	case "", "toggle":
		b.ToggleRecording()
	case "start", "on":
		b.StartRecording()
	case "stop", "off":
		b.StopRecording(true)
	default:
		b.AddOutputLine("Usage: /record [start|stop|toggle]")
	}
}

func (b *Barnard) ToggleRecording() {
	b.RecordingMutex.Lock()
	active := b.Recorder != nil || b.recordingStarting
	b.RecordingMutex.Unlock()
	if active {
		b.StopRecording(true)
	} else {
		b.StartRecording()
	}
}

func (b *Barnard) StartRecording() {
	if b.Client == nil || b.Client.Self == nil {
		b.AddOutputLine("Recording requires an active server connection")
		b.Notify("recorderror", "me", "Recording requires an active server connection")
		return
	}
	b.RecordingMutex.Lock()
	if b.recordingAllowed != nil && !*b.recordingAllowed {
		b.RecordingMutex.Unlock()
		b.AddOutputLine("Recording is not allowed by this server.")
		b.Notify("recorderror", "me", "Recording is not allowed by this server.")
		return
	}
	if b.Recorder != nil || b.recordingStarting {
		b.RecordingMutex.Unlock()
		b.AddOutputLine("Recording is already active")
		return
	}
	b.recordingStarting = true
	b.RecordingMutex.Unlock()

	b.Client.Self.SetRecording(true)
	b.AddOutputLine("Recording start requested")
	b.renderGeneralStatus()
}

func (b *Barnard) StopRecording(notifyServer bool) {
	recorder, path, wasPending := b.detachRecorder()
	if recorder == nil {
		if notifyServer && b.Client != nil && b.Client.Self != nil {
			b.Client.Self.SetRecording(false)
		}
		if wasPending {
			b.AddOutputLine("Recording start cancelled")
			b.Notify("recordstop", "me", "Recording start cancelled")
		} else {
			b.AddOutputLine("Recording is not active")
		}
		b.renderGeneralStatus()
		return
	}

	if err := recorder.Stop(); err != nil {
		b.AddOutputLine(fmt.Sprintf("Recording stopped with error: %s", err))
		b.Notify("recorderror", "me", err.Error())
	} else {
		b.AddOutputLine(fmt.Sprintf("Recording saved: %s", path))
		b.Notify("recordstop", "me", path)
	}
	if notifyServer && b.Client != nil && b.Client.Self != nil {
		b.Client.Self.SetRecording(false)
	}
	b.renderGeneralStatus()
}

func (b *Barnard) StopRecordingIfActive(notifyServer bool) {
	b.RecordingMutex.Lock()
	active := b.Recorder != nil || b.recordingStarting
	b.RecordingMutex.Unlock()
	if active {
		b.StopRecording(notifyServer)
	}
}

func (b *Barnard) HandleRecordingChange(e *gumble.UserChangeEvent) {
	if e.User == nil {
		return
	}
	if e.User == b.Client.Self {
		if e.User.Recording {
			b.finishRecordingStart()
		} else {
			b.RecordingMutex.Lock()
			hadRecorder := b.Recorder != nil
			b.RecordingMutex.Unlock()
			if hadRecorder {
				b.StopRecording(false)
			}
		}
		return
	}
	if e.User.Recording {
		b.AddOutputLine(fmt.Sprintf("%s started recording", e.User.Name))
		b.Notify("userrecordstart", e.User.Name, "")
	} else {
		b.AddOutputLine(fmt.Sprintf("%s stopped recording", e.User.Name))
		b.Notify("userrecordstop", e.User.Name, "")
	}
}

func (b *Barnard) HandleRecordingAllowed(allowed *bool) {
	if allowed == nil {
		return
	}
	b.RecordingMutex.Lock()
	value := *allowed
	b.recordingAllowed = &value
	active := b.Recorder != nil || b.recordingStarting
	b.RecordingMutex.Unlock()
	if !value && active {
		b.AddOutputLine("Recording is not allowed by this server.")
		b.Notify("recorderror", "me", "Recording is not allowed by this server.")
		b.StopRecording(true)
	}
}

func (b *Barnard) finishRecordingStart() {
	b.RecordingMutex.Lock()
	if !b.recordingStarting || b.Recorder != nil {
		b.RecordingMutex.Unlock()
		return
	}
	b.RecordingMutex.Unlock()

	recorder, err := recording.New(
		b.UserConfig.GetRecordingDirectory(),
		b.UserConfig.GetRecordingFormat(),
		time.Now(),
		b.Client.Config.AudioFrameSize(),
		b.Client.Config.AudioInterval,
	)
	if err != nil {
		b.RecordingMutex.Lock()
		b.recordingStarting = false
		b.RecordingMutex.Unlock()
		b.AddOutputLine(fmt.Sprintf("Could not start recording: %s", err))
		b.Notify("recorderror", "me", err.Error())
		b.Client.Self.SetRecording(false)
		b.renderGeneralStatus()
		return
	}

	b.RecordingMutex.Lock()
	if !b.recordingStarting {
		b.RecordingMutex.Unlock()
		recorder.Stop()
		return
	}
	b.Recorder = recorder
	b.recordingStarting = false
	b.RecordingMutex.Unlock()
	if b.Stream != nil {
		b.Stream.SetRecorder(recorder)
	}
	b.AddOutputLine(fmt.Sprintf("Recording started: %s", recorder.Path()))
	b.Notify("recordstart", "me", recorder.Path())
	b.renderGeneralStatus()
}

func (b *Barnard) isRecordingActive() bool {
	b.RecordingMutex.Lock()
	defer b.RecordingMutex.Unlock()
	return b.Recorder != nil || b.recordingStarting
}

func (b *Barnard) detachRecorder() (*recording.Recorder, string, bool) {
	b.RecordingMutex.Lock()
	defer b.RecordingMutex.Unlock()
	recorder := b.Recorder
	wasPending := b.recordingStarting
	path := ""
	if recorder != nil {
		path = recorder.Path()
	}
	b.Recorder = nil
	b.recordingStarting = false
	if b.Stream != nil {
		b.Stream.SetRecorder(nil)
	}
	return recorder, path, wasPending
}

func (b *Barnard) stopRecordingForDisconnect() {
	recorder, path, _ := b.detachRecorder()
	if recorder != nil {
		if err := recorder.Stop(); err != nil {
			b.AddOutputLine(fmt.Sprintf("Recording stopped with error: %s", err))
			b.Notify("recorderror", "me", err.Error())
		} else {
			b.AddOutputLine(fmt.Sprintf("Recording saved: %s", path))
			b.Notify("recordstop", "me", path)
		}
	}
	b.renderGeneralStatus()
}
