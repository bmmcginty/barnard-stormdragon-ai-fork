package gumble

import (
	"testing"
)

type serverConfigListener struct {
	EventListener
	event *ServerConfigEvent
}

func (l *serverConfigListener) OnServerConfig(e *ServerConfigEvent) {
	l.event = e
}

func TestHandleServerConfigRecordingAllowed(t *testing.T) {
	data := []byte{56, 0} // field 7, varint false

	client := &Client{Config: NewConfig()}
	listener := &serverConfigListener{}
	client.Config.Attach(listener)

	if err := client.handleServerConfig(data); err != nil {
		t.Fatal(err)
	}
	if listener.event == nil {
		t.Fatal("expected server config event")
	}
	if listener.event.RecordingAllowed == nil {
		t.Fatal("expected recording allowed value")
	}
	if *listener.event.RecordingAllowed {
		t.Fatal("expected recording to be disallowed")
	}
}
