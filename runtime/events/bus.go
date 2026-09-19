package events

import (
	"sync"
	"sync/atomic"
	"time"
)

type EventID uint64
type EventType string
type Component string
type RuntimeEvent struct {
	ID        EventID
	Type      EventType
	Timestamp time.Time
	Source    Component
	SessionID string
	Payload   any
}
type Handler func(RuntimeEvent)
type subscription struct {
	handler Handler
	active  atomic.Bool
}

// Bus serializes domain handlers. Publish never waits for a handler, including
// when a handler publishes follow-up events. Observers must enqueue slow I/O.
type Bus struct {
	sessionID   string
	sequence    uint64
	mu          sync.Mutex
	cond        *sync.Cond
	subscribers map[EventType][]*subscription
	wildcards   []*subscription
	queue       []RuntimeEvent
	closed      bool
	overloaded  bool
	done        chan struct{}
}

func NewBus(sessionID string) *Bus {
	b := &Bus{sessionID: sessionID, subscribers: make(map[EventType][]*subscription), done: make(chan struct{})}
	b.cond = sync.NewCond(&b.mu)
	go b.dispatch()
	return b
}
func (b *Bus) SessionID() string                       { return b.sessionID }
func (b *Bus) Subscribe(t EventType, h Handler) func() { return b.add(t, h, false) }
func (b *Bus) SubscribeAll(h Handler) func()           { return b.add("", h, true) }
func (b *Bus) add(t EventType, h Handler, all bool) func() {
	s := &subscription{handler: h}
	s.active.Store(true)
	b.mu.Lock()
	if all {
		b.wildcards = append(b.wildcards, s)
	} else {
		b.subscribers[t] = append(b.subscribers[t], s)
	}
	b.mu.Unlock()
	return func() {
		s.active.Store(false)
		b.mu.Lock()
		defer b.mu.Unlock()
		list := b.subscribers[t]
		if all {
			list = b.wildcards
		}
		for i, entry := range list {
			if entry == s {
				list = append(list[:i], list[i+1:]...)
				break
			}
		}
		if all {
			b.wildcards = list
		} else {
			b.subscribers[t] = list
		}
	}
}
func (b *Bus) Publish(t EventType, source Component, payload any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	if b.overloaded && t != "RuntimeShutdown" {
		return
	}
	// Logs are observational; discard excess diagnostics rather than allowing
	// a verbose child to crowd out lifecycle events and cancellation.
	if t == "WorkerLog" && len(b.queue) >= 1024 {
		return
	}
	if len(b.queue) >= 4096 {
		// There is no safe way to block a reentrant publisher. Fail the runtime
		// explicitly instead of deadlocking or silently losing a required event.
		b.overloaded = true
		b.queue = nil
		b.sequence++
		b.queue = append(b.queue, RuntimeEvent{EventID(b.sequence), "RuntimeOverloaded", time.Now(), "event_bus", b.sessionID, map[string]any{"reason": "event_queue_capacity", "restart_required": true}})
		b.cond.Signal()
		return
	}
	b.sequence++
	b.queue = append(b.queue, RuntimeEvent{EventID(b.sequence), t, time.Now(), source, b.sessionID, payload})
	b.cond.Signal()
}
func (b *Bus) dispatch() {
	defer close(b.done)
	for {
		b.mu.Lock()
		for len(b.queue) == 0 && !b.closed {
			b.cond.Wait()
		}
		if len(b.queue) == 0 && b.closed {
			b.mu.Unlock()
			return
		}
		e := b.queue[0]
		b.queue[0] = RuntimeEvent{}
		b.queue = b.queue[1:]
		handlers := append([]*subscription{}, b.subscribers[e.Type]...)
		handlers = append(handlers, b.wildcards...)
		b.mu.Unlock()
		for _, s := range handlers {
			if s.active.Load() {
				b.invoke(s, e)
			}
		}
	}
}

func (b *Bus) invoke(s *subscription, event RuntimeEvent) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// A subscriber is an isolation boundary. Quarantine the broken
			// callback and report it through the surviving observers instead of
			// silently losing the sole dispatch goroutine.
			s.active.Store(false)
			b.Publish("SubscriberPanicked", "event_bus", map[string]any{
				"event_type": string(event.Type),
				"error":      "subscriber panicked",
			})
		}
	}()
	s.handler(event)
}

// Close rejects new work and drains queued events. Call outside a handler.
func (b *Bus) Close() { b.mu.Lock(); b.closed = true; b.cond.Broadcast(); b.mu.Unlock(); <-b.done }

func (b *Bus) Accepting() bool { b.mu.Lock(); defer b.mu.Unlock(); return !b.closed && !b.overloaded }
