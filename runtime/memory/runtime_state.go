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
	return fmt.Sprintf("%d:%s%d:%s%d:%s", len(scope), scope, len(scopeID), scopeID, len(key), key)
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

	entry.Value = cloneValue(entry.Value)
	s.store[composite] = entry
}

// Get reads an entry from shared memory by its scope and key.
func (s *RuntimeState) Get(scope MemoryScope, scopeID string, key string) (MemoryEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	composite := computeKey(scope, scopeID, key)
	val, ok := s.store[composite]
	val.Value = cloneValue(val.Value)
	return val, ok
}

// GetAllByScope returns all memory entries for a particular scope and scopeID.
func (s *RuntimeState) GetAllByScope(scope MemoryScope, scopeID string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make(map[string]any)
	prefix := fmt.Sprintf("%d:%s%d:%s", len(scope), scope, len(scopeID), scopeID)

	for k, v := range s.store {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			results[v.Key] = cloneValue(v.Value)
		}
	}
	return results
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		y := make(map[string]any, len(x))
		for k, v := range x {
			y[k] = cloneValue(v)
		}
		return y
	case []any:
		y := make([]any, len(x))
		for i, v := range x {
			y[i] = cloneValue(v)
		}
		return y
	case []string:
		return append([]string(nil), x...)
	case map[string]string:
		y := make(map[string]string, len(x))
		for k, v := range x {
			y[k] = v
		}
		return y
	default:
		return v
	}
}
