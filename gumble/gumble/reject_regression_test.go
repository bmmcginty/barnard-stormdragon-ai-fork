package gumble

import (
	"io"
	"net"
	"testing"
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble/MumbleProto"
	"google.golang.org/protobuf/proto"
)

// Regression: the connect channel was unbuffered, so a Reject arriving after
// DialWithDialer had already returned on its synchronization timeout blocked
// readRoutine forever, leaking that goroutine and the whole client with it.
func TestHandleRejectDoesNotBlockWithoutAReceiver(t *testing.T) {
	c := &Client{
		Config:  NewConfig(),
		Users:   make(Users),
		connect: make(chan *RejectError, 1),
		state:   uint32(StateConnected),
	}
	c.Conn = NewConn(nopConn{})

	reason := "server is full"
	data, _ := proto.Marshal(&MumbleProto.Reject{Reason: &reason})

	done := make(chan struct{})
	go func() {
		_ = c.handleReject(data)
		close(done)
	}()

	select {
	case <-done:
	case <-timeoutChan():
		t.Fatal("handleReject blocked with no reader on the connect channel")
	}
}

// nopConn is a net.Conn that discards everything, so handleReject's Close call
// has something to act on.
type nopConn struct{}

func (nopConn) Read(b []byte) (int, error)         { return 0, io.EOF }
func (nopConn) Write(b []byte) (int, error)        { return len(b), nil }
func (nopConn) Close() error                       { return nil }
func (nopConn) LocalAddr() net.Addr                { return nil }
func (nopConn) RemoteAddr() net.Addr               { return nil }
func (nopConn) SetDeadline(t time.Time) error      { return nil }
func (nopConn) SetReadDeadline(t time.Time) error  { return nil }
func (nopConn) SetWriteDeadline(t time.Time) error { return nil }

func timeoutChan() <-chan time.Time {
	return time.After(10 * time.Second)
}
