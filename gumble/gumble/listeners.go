package gumble

import "sync"

type eventItem struct {
	parent     *Listeners
	prev, next *eventItem
	listener   EventListener
	detached   bool
}

func (e *eventItem) Detach() {
	e.parent.mu.Lock()
	defer e.parent.mu.Unlock()
	if e.detached {
		return
	}
	e.detached = true
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

// Listeners is a list of event listeners. Delivery uses a snapshot, so an
// attach or detach from another goroutine cannot corrupt iteration.
type Listeners struct {
	mu         sync.Mutex
	head, tail *eventItem
}

func (e *Listeners) Attach(listener EventListener) Detacher {
	e.mu.Lock()
	defer e.mu.Unlock()
	item := &eventItem{parent: e, prev: e.tail, listener: listener}
	if e.head == nil {
		e.head = item
	}
	if e.tail != nil {
		e.tail.next = item
	}
	e.tail = item
	return item
}

func (e *Listeners) dispatch(f func(EventListener)) {
	e.mu.Lock()
	listeners := make([]EventListener, 0)
	for item := e.head; item != nil; item = item.next {
		listeners = append(listeners, item.listener)
	}
	e.mu.Unlock()
	for _, listener := range listeners {
		f(listener)
	}
}

func (e *Listeners) onConnect(event *ConnectEvent) {
	e.dispatch(func(l EventListener) { l.OnConnect(event) })
}
func (e *Listeners) onDisconnect(event *DisconnectEvent) {
	e.dispatch(func(l EventListener) { l.OnDisconnect(event) })
}
func (e *Listeners) onTextMessage(event *TextMessageEvent) {
	e.dispatch(func(l EventListener) { l.OnTextMessage(event) })
}
func (e *Listeners) onUserChange(event *UserChangeEvent) {
	e.dispatch(func(l EventListener) { l.OnUserChange(event) })
}
func (e *Listeners) onChannelChange(event *ChannelChangeEvent) {
	e.dispatch(func(l EventListener) { l.OnChannelChange(event) })
}
func (e *Listeners) onPermissionDenied(event *PermissionDeniedEvent) {
	e.dispatch(func(l EventListener) { l.OnPermissionDenied(event) })
}
func (e *Listeners) onUserList(event *UserListEvent) {
	e.dispatch(func(l EventListener) { l.OnUserList(event) })
}
func (e *Listeners) onACL(event *ACLEvent) { e.dispatch(func(l EventListener) { l.OnACL(event) }) }
func (e *Listeners) onBanList(event *BanListEvent) {
	e.dispatch(func(l EventListener) { l.OnBanList(event) })
}
func (e *Listeners) onContextActionChange(event *ContextActionChangeEvent) {
	e.dispatch(func(l EventListener) { l.OnContextActionChange(event) })
}
func (e *Listeners) onServerConfig(event *ServerConfigEvent) {
	e.dispatch(func(l EventListener) { l.OnServerConfig(event) })
}
