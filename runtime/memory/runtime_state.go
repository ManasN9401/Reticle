package memory

import (
	"sync"
)

// RuntimeState provides a thread-safe KV store representing ephemeral runtime state.
// It physically prevents external mutation by hiding the .set() method.
type RuntimeState struct {
	store map[MemoryKey]any
	mu    sync.RWMutex
}

func NewRuntimeState() *RuntimeState {
	return &RuntimeState{
		store: make(map[MemoryKey]any),
	}
}

// set writes a value to shared memory. Unexported to enforce Event Bus mutation.
func (s *RuntimeState) set(key MemoryKey, val any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[key] = val
}

// Get reads a value from shared memory. Safe for public read-only access.
func (s *RuntimeState) Get(key MemoryKey) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.store[key]
	return val, ok
}
