package gumble

import "sync"

type audioEventItem struct {
	parent     *AudioListeners
	prev, next *audioEventItem
	listener   AudioListener
	streams    map[*User]chan *AudioPacket
	detached   bool
}

func (e *audioEventItem) Detach() {
	e.parent.mu.Lock()
	defer e.parent.mu.Unlock()
	if e.detached {
		return
	}
	e.detached = true
	for user, stream := range e.streams {
		close(stream)
		delete(e.streams, user)
	}
	if e.prev == nil {
		e.parent.head = e.next
	} else {
		e.prev.next = e.next
	}
	if e.next == nil {
		e.parent.tail = e.prev
	} else {
		e.next.prev = e.prev
	}
	e.prev, e.next = nil, nil
}

// AudioListeners is a list of audio listeners. Each attached listener is
// called in sequence when a new user audio stream begins.
type AudioListeners struct {
	mu         sync.Mutex
	head, tail *audioEventItem
}

// Attach adds a new audio listener to the end of the current list of listeners.
func (e *AudioListeners) Attach(listener AudioListener) Detacher {
	e.mu.Lock()
	defer e.mu.Unlock()
	item := &audioEventItem{
		parent:   e,
		prev:     e.tail,
		listener: listener,
		streams:  make(map[*User]chan *AudioPacket),
	}
	if e.head == nil {
		e.head = item
	}
	if e.tail != nil {
		e.tail.next = item
	}
	// tail was previously left pointing at the first item ever attached. Once
	// anything detached, the next attach linked itself onto a node that was no
	// longer in the list, so that listener never received audio again.
	e.tail = item
	return item
}
