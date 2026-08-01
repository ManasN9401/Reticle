package memory

import (
	"fmt"
	"sync"
	"time"
)

// RuntimeState provides a thread-safe KV store representing ephemeral runtime state.
// It physically prevents external mutation by hiding the .set() method.
type RuntimeState struct {
	store map[string]MemoryEntry
	mu    sync.RWMutex
}

func NewRuntimeState() *RuntimeState {
	return &RuntimeState{
		store: make(map[string]MemoryEntry),
	}
}

// computeKey generates a deterministic scoped key.
func computeKey(scope MemoryScope, scopeID string, key string) string {
	return fmt.Sprintf("%s:%s:%s", scope, scopeID, key)
}

// set writes a MemoryEntry to shared memory. Unexported to enforce Event Bus mutation.
func (s *RuntimeState) set(entry MemoryEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	composite := computeKey(entry.Scope, entry.ScopeID, entry.Key)
	now := time.Now()

	if existing, ok := s.store[composite]; ok {
		entry.CreatedAt = existing.CreatedAt
	} else {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now

	s.store[composite] = entry
}

// Get reads an entry from shared memory by its scope and key.
func (s *RuntimeState) Get(scope MemoryScope, scopeID string, key string) (MemoryEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	composite := computeKey(scope, scopeID, key)
	val, ok := s.store[composite]
	return val, ok
}

// GetAllByScope returns all memory entries for a particular scope and scopeID.
func (s *RuntimeState) GetAllByScope(scope MemoryScope, scopeID string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	results := make(map[string]any)
	prefix := fmt.Sprintf("%s:%s:", scope, scopeID)
	
	for k, v := range s.store {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			results[v.Key] = v.Value
		}
	}
	return results
}
