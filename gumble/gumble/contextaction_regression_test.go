package gumble

import (
	"net"
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumble/MumbleProto"
	"google.golang.org/protobuf/proto"
)

// Regression: server context-action adds wrote to a nil map and the resulting
// action had no owning client, so Trigger panicked.
func TestContextActionAddAndTrigger(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	c := &Client{Config: NewConfig(), Users: make(Users), Channels: make(Channels), ContextActions: make(ContextActions)}
	c.Conn = NewConn(clientConn)
	action, operation := "test", MumbleProto.ContextActionModify_Add
	data, err := proto.Marshal(&MumbleProto.ContextActionModify{Action: &action, Operation: &operation})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handleContextActionModify(data); err != nil {
		t.Fatal(err)
	}
	added := c.ContextActions[action]
	if added == nil || added.client != c {
		t.Fatal("action was not initialized with its client")
	}
	written := make(chan error, 1)
	go func() { _, _, err := NewConn(serverConn).ReadPacket(); written <- err }()
	added.Trigger()
	if err := <-written; err != nil {
		t.Fatalf("trigger did not write: %v", err)
	}
	remove := MumbleProto.ContextActionModify_Remove
	data, _ = proto.Marshal(&MumbleProto.ContextActionModify{Action: &action, Operation: &remove})
	if err := c.handleContextActionModify(data); err != nil {
		t.Fatal(err)
	}
	if c.ContextActions[action] != nil {
		t.Fatal("action was not removed")
	}
}

// Regression: the documented bit layout disagreed with SemanticVersion.
func TestSemanticVersionKnownLayout(t *testing.T) {
	major, minor, patch := (&Version{Version: 1<<16 | 5<<8 | 2}).SemanticVersion()
	if major != 1 || minor != 5 || patch != 2 {
		t.Fatalf("got %d.%d.%d", major, minor, patch)
	}
}
