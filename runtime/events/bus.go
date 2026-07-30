package events

import (
	"sync"
	"sync/atomic"
	"time"
)

// Domain Types
type EventID uint64
type EventType string
type Component string

// RuntimeEvent represents a standardized, significant runtime occurrence.
type RuntimeEvent struct {
	ID        EventID
	Type      EventType
	Timestamp time.Time
	Source    Component
	SessionID string
	Payload   any
}

// Handler is a function that reacts to an event.
type Handler func(RuntimeEvent)

// Bus provides an asynchronous publish/subscribe mechanism.
type Bus struct {
	sessionID   string
	sequence    atomic.Uint64
	subscribers map[EventType][]Handler
	wildcards   []Handler
	mu          sync.RWMutex
	eventChan   chan RuntimeEvent
}

func NewBus(sessionID string) *Bus {
	b := &Bus{
		sessionID:   sessionID,
		subscribers: make(map[EventType][]Handler),
		eventChan:   make(chan RuntimeEvent, 1000), // Buffered channel to prevent blocking publishers
	}
	go b.startDispatcher()
	return b
}

func (b *Bus) SessionID() string {
	return b.sessionID
}

func (b *Bus) startDispatcher() {
	for event := range b.eventChan {
		b.mu.RLock()
		handlers := b.subscribers[event.Type]
		wildcards := b.wildcards
		b.mu.RUnlock()

		for _, handler := range handlers {
			go handler(event) // Execute handlers asynchronously
		}
		
		for _, handler := range wildcards {
			handler(event) // Execute synchronously to guarantee chronological auditing/logging
		}
	}
}

// Subscribe registers a handler for a specific event type.
func (b *Bus) Subscribe(eventType EventType, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[eventType] = append(b.subscribers[eventType], handler)
}

// SubscribeAll registers a handler for all events.
func (b *Bus) SubscribeAll(handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wildcards = append(b.wildcards, handler)
}

// Publish constructs a RuntimeEvent and asynchronously delivers it to the bus.
func (b *Bus) Publish(eventType EventType, source Component, payload any) {
	event := RuntimeEvent{
		ID:        EventID(b.sequence.Add(1)),
		Type:      eventType,
		Timestamp: time.Now(),
		Source:    source,
		SessionID: b.sessionID,
		Payload:   payload,
	}
	b.eventChan <- event
}
