package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// RuntimeState provides a thread-safe KV store representing ephemeral runtime state.
// Writes are internal; reads and writes clone supported JSON-shaped values.
// Arbitrary custom Go pointers are outside this ownership contract.
type RuntimeState struct {
	store     map[string]map[string]MemoryEntry
	mu        sync.RWMutex
	policy    RetentionPolicy
	bytes     int64
	entries   int
	now       func() time.Time
	nextPrune time.Time
}

var (
	ErrVersionConflict = errors.New("memory version conflict")
	ErrMemoryCapacity  = errors.New("memory retention capacity exceeded")
)

func NewRuntimeState() *RuntimeState {
	return NewRuntimeStateWithPolicy(DefaultRetentionPolicy())
}

func NewRuntimeStateWithPolicy(policy RetentionPolicy) *RuntimeState {
	defaults := DefaultRetentionPolicy()
	if policy.MaxEntries <= 0 {
		policy.MaxEntries = defaults.MaxEntries
	}
	if policy.MaxBytes <= 0 {
		policy.MaxBytes = defaults.MaxBytes
	}
	if policy.ExecutionTTL <= 0 {
		policy.ExecutionTTL = defaults.ExecutionTTL
	}
	if policy.MaxArtifactVersions <= 0 {
		policy.MaxArtifactVersions = defaults.MaxArtifactVersions
	}
	return &RuntimeState{
		store:  make(map[string]map[string]MemoryEntry),
		policy: policy,
		now:    time.Now,
	}
}

// computeKey generates a deterministic scoped key.
func computeKey(scope MemoryScope, scopeID string, key string) string {
	return fmt.Sprintf("%d:%s%d:%s%d:%s", len(scope), scope, len(scopeID), scopeID, len(key), key)
}

// set writes a MemoryEntry to shared memory. Unexported to enforce Event Bus mutation.
func (s *RuntimeState) set(entry MemoryEntry) (MemoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setLocked(entry)
}

func (s *RuntimeState) setLocked(entry MemoryEntry) (MemoryEntry, error) {
	if entry.Key == "" || entry.ScopeID == "" || !validScope(entry.Scope) {
		return MemoryEntry{}, fmt.Errorf("invalid memory address")
	}
	now := s.now()
	if s.nextPrune.IsZero() || !now.Before(s.nextPrune) {
		s.pruneExpiredLocked(now)
		s.nextPrune = now.Add(time.Minute)
	}

	composite := computeKey(entry.Scope, entry.ScopeID, "")
	bucket := s.store[composite]
	if bucket == nil {
		bucket = make(map[string]MemoryEntry)
		s.store[composite] = bucket
	}
	existing, exists := bucket[entry.Key]
	if entry.ExpectedVersion != nil {
		actual := uint64(0)
		if exists {
			actual = existing.Version
		}
		if actual != *entry.ExpectedVersion {
			return MemoryEntry{}, fmt.Errorf("%w: expected %d, actual %d", ErrVersionConflict, *entry.ExpectedVersion, actual)
		}
	}
	if exists {
		entry.CreatedAt = existing.CreatedAt
		entry.Version = existing.Version + 1
	} else {
		entry.CreatedAt = now
		entry.Version = 1
	}
	entry.UpdatedAt = now
	entry.ExpectedVersion = nil
	if entry.ExpiresAt == nil && entry.Scope == ScopeExecution {
		expires := now.Add(s.policy.ExecutionTTL)
		entry.ExpiresAt = &expires
	}

	entry.Value = cloneValue(entry.Value)
	newSize, err := entrySize(entry)
	if err != nil {
		return MemoryEntry{}, fmt.Errorf("memory value must be JSON serializable: %w", err)
	}
	oldSize := int64(0)
	if exists {
		oldSize, _ = entrySize(existing)
	}
	if (!exists && s.entries >= s.policy.MaxEntries) || s.bytes-oldSize+newSize > s.policy.MaxBytes {
		return MemoryEntry{}, ErrMemoryCapacity
	}
	bucket[entry.Key] = entry
	s.bytes = s.bytes - oldSize + newSize
	if !exists {
		s.entries++
	}
	return cloneEntry(entry), nil
}

// Get reads an entry from shared memory by its scope and key.
func (s *RuntimeState) Get(scope MemoryScope, scopeID string, key string) (MemoryEntry, bool) {
	now := s.now()
	s.mu.RLock()
	composite := computeKey(scope, scopeID, "")
	val, ok := s.store[composite][key]
	s.mu.RUnlock()
	if ok && val.ExpiresAt != nil && !val.ExpiresAt.After(now) {
		s.deleteIfExpired(composite, key, now)
		return MemoryEntry{}, false
	}
	return cloneEntry(val), ok
}

// GetAllByScope returns all memory entries for a particular scope and scopeID.
func (s *RuntimeState) GetAllByScope(scope MemoryScope, scopeID string) map[string]any {
	now := s.now()
	s.mu.RLock()
	bucket := s.store[computeKey(scope, scopeID, "")]
	results := make(map[string]any, len(bucket))
	hasExpired := false
	for k, v := range bucket {
		if v.ExpiresAt != nil && !v.ExpiresAt.After(now) {
			hasExpired = true
			continue
		}
		results[k] = cloneValue(v.Value)
	}
	s.mu.RUnlock()
	if hasExpired {
		s.mu.Lock()
		s.pruneExpiredLocked(now)
		s.mu.Unlock()
	}
	return results
}

func (s *RuntimeState) deleteIfExpired(bucketKey, key string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.store[bucketKey]
	entry, ok := bucket[key]
	if !ok || entry.ExpiresAt == nil || entry.ExpiresAt.After(now) {
		return
	}
	size, _ := entrySize(entry)
	s.bytes -= size
	delete(bucket, key)
	s.entries--
	if len(bucket) == 0 {
		delete(s.store, bucketKey)
	}
}

func (s *RuntimeState) DeleteScope(scope MemoryScope, scopeID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucketKey := computeKey(scope, scopeID, "")
	bucket := s.store[bucketKey]
	for _, entry := range bucket {
		size, _ := entrySize(entry)
		s.bytes -= size
	}
	delete(s.store, bucketKey)
	s.entries -= len(bucket)
	return len(bucket)
}

func (s *RuntimeState) rollback(entry MemoryEntry, previous MemoryEntry, existed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucketKey := computeKey(entry.Scope, entry.ScopeID, "")
	bucket := s.store[bucketKey]
	if current, ok := bucket[entry.Key]; ok {
		size, _ := entrySize(current)
		s.bytes -= size
		delete(bucket, entry.Key)
		s.entries--
	}
	if existed {
		if bucket == nil {
			bucket = make(map[string]MemoryEntry)
			s.store[bucketKey] = bucket
		}
		bucket[entry.Key] = cloneEntry(previous)
		size, _ := entrySize(previous)
		s.bytes += size
		s.entries++
	}
	if len(bucket) == 0 {
		delete(s.store, bucketKey)
	}
}

func (s *RuntimeState) Snapshot() []MemoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(s.now())
	entries := make([]MemoryEntry, 0, s.entries)
	for _, bucket := range s.store {
		for _, entry := range bucket {
			entries = append(entries, cloneEntry(entry))
		}
	}
	return entries
}

func (s *RuntimeState) Restore(entries []MemoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = make(map[string]map[string]MemoryEntry)
	s.bytes = 0
	s.entries = 0
	s.nextPrune = s.now().Add(time.Minute)
	for _, entry := range entries {
		if entry.ExpiresAt != nil && !entry.ExpiresAt.After(s.now()) {
			continue
		}
		if entry.Key == "" || entry.ScopeID == "" || !validScope(entry.Scope) {
			return fmt.Errorf("invalid restored memory entry")
		}
		if entry.Version == 0 {
			entry.Version = 1
		}
		key := computeKey(entry.Scope, entry.ScopeID, "")
		if s.store[key] == nil {
			s.store[key] = make(map[string]MemoryEntry)
		}
		entry.ExpectedVersion = nil
		entry.Value = cloneValue(entry.Value)
		size, err := entrySize(entry)
		if err != nil {
			return err
		}
		if s.entries >= s.policy.MaxEntries || s.bytes+size > s.policy.MaxBytes {
			return ErrMemoryCapacity
		}
		s.store[key][entry.Key] = entry
		s.bytes += size
		s.entries++
	}
	return nil
}

func validScope(scope MemoryScope) bool {
	return scope == ScopeGlobal || scope == ScopeWorkflow || scope == ScopeExecution || scope == ScopeAgent
}
func entrySize(entry MemoryEntry) (int64, error) {
	data, err := json.Marshal(entry)
	return int64(len(data)), err
}
func cloneEntry(entry MemoryEntry) MemoryEntry {
	entry.Value = cloneValue(entry.Value)
	if entry.ExpiresAt != nil {
		t := *entry.ExpiresAt
		entry.ExpiresAt = &t
	}
	entry.ExpectedVersion = nil
	return entry
}
func (s *RuntimeState) pruneExpiredLocked(now time.Time) {
	for bucketKey, bucket := range s.store {
		for key, entry := range bucket {
			if entry.ExpiresAt != nil && !entry.ExpiresAt.After(now) {
				size, _ := entrySize(entry)
				s.bytes -= size
				delete(bucket, key)
				s.entries--
			}
		}
		if len(bucket) == 0 {
			delete(s.store, bucketKey)
		}
	}
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
