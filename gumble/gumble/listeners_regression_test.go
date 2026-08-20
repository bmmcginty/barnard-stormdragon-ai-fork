package gumble

import (
	"sync"
	"testing"
)

// Regression: Detach modified the linked listener list without synchronization
// and a second detach could corrupt its head/tail links.
func TestListenerDetachIsIdempotentAndConcurrent(t *testing.T) {
	var listeners Listeners
	item := listeners.Attach(nil)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); item.Detach() }()
	}
	wg.Wait()
	listeners.mu.Lock()
	defer listeners.mu.Unlock()
	if listeners.head != nil || listeners.tail != nil {
		t.Fatal("detached listener remains linked")
	}
}

// Regression: audio listener detach could be invoked twice while dispatch was
// active, leaving list links inconsistent.
func TestAudioListenerDetachIsIdempotent(t *testing.T) {
	var listeners AudioListeners
	item := listeners.Attach(nil)
	stream := make(chan *AudioPacket)
	item.(*audioEventItem).streams[&User{}] = stream
	item.Detach()
	item.Detach()
	if _, open := <-stream; open {
		t.Fatal("detached audio listener stream remained open")
	}
	listeners.mu.Lock()
	defer listeners.mu.Unlock()
	if listeners.head != nil || listeners.tail != nil {
		t.Fatal("detached audio listener remains linked")
	}
}
