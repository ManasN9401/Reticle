package memory

import (
	"fmt"
	"path/filepath"
	"sync"
)

type FileLock struct {
	mu        sync.Mutex
	SessionID string // The session that currently holds the lock
}

type MutexStore struct {
	locks map[string]*FileLock
	mu    sync.Mutex // Protects the locks map
}

func NewMutexStore() *MutexStore {
	return &MutexStore{
		locks: make(map[string]*FileLock),
	}
}

// RequestLock blocks until the file lock is acquired by the session.
// Returns an error if the path is invalid.
func (ms *MutexStore) RequestLock(sessionID string, filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("invalid path: %v", err)
	}

	ms.mu.Lock()
	lock, exists := ms.locks[absPath]
	if !exists {
		lock = &FileLock{}
		ms.locks[absPath] = lock
	}
	ms.mu.Unlock()

	// Block until acquired
	lock.mu.Lock()
	
	ms.mu.Lock()
	lock.SessionID = sessionID
	ms.mu.Unlock()
	
	return nil
}

// ReleaseLock frees the file lock.
func (ms *MutexStore) ReleaseLock(sessionID string, filePath string) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return
	}

	ms.mu.Lock()
	lock, exists := ms.locks[absPath]
	if exists && lock.SessionID == sessionID {
		lock.SessionID = ""
		lock.mu.Unlock()
	}
	ms.mu.Unlock()
}

// ReleaseAllBySession forcefully releases all locks held by a crashed or finished session.
func (ms *MutexStore) ReleaseAllBySession(sessionID string) {
	ms.mu.Lock()
	for _, lock := range ms.locks {
		if lock.SessionID == sessionID {
			lock.SessionID = ""
			lock.mu.Unlock()
		}
	}
	ms.mu.Unlock()
}
