package memory

import (
	"fmt"
	"sync"
	"time"
)

// ArtifactStore maintains the in-memory knowledge graph and specialized indexes.
// It tracks the full history of every ArtifactID.
type ArtifactStore struct {
	store      map[ArtifactID][]*Artifact
	byProducer map[string][]ArtifactID
	byType     map[ArtifactType][]ArtifactID
	byParent   map[ArtifactID][]ArtifactID
	mu         sync.RWMutex
}

func NewArtifactStore() *ArtifactStore {
	return &ArtifactStore{
		store:      make(map[ArtifactID][]*Artifact),
		byProducer: make(map[string][]ArtifactID),
		byType:     make(map[ArtifactType][]ArtifactID),
		byParent:   make(map[ArtifactID][]ArtifactID),
	}
}

// save physically writes the artifact, auto-increments version, and updates indexes.
func (as *ArtifactStore) save(artifact *Artifact) {
	as.mu.Lock()
	defer as.mu.Unlock()

	if artifact.StoredAt == nil {
		now := time.Now()
		artifact.StoredAt = &now
	}

	history := as.store[artifact.ID]

	// Auto-increment version based on history length
	artifact.Version = uint32(len(history) + 1)

	as.store[artifact.ID] = append(history, cloneArtifact(artifact))

	// If this is the first version, update secondary indexes
	if len(history) > 0 {
		old := history[len(history)-1]
		as.byProducer[old.Producer] = removeID(as.byProducer[old.Producer], artifact.ID)
		as.byType[old.Type] = removeID(as.byType[old.Type], artifact.ID)
		for _, p := range old.Parents {
			as.byParent[p] = removeID(as.byParent[p], artifact.ID)
		}
	}
	if true {
		as.byProducer[artifact.Producer] = append(as.byProducer[artifact.Producer], artifact.ID)
		as.byType[artifact.Type] = append(as.byType[artifact.Type], artifact.ID)

		for _, parentID := range artifact.Parents {
			as.byParent[parentID] = append(as.byParent[parentID], artifact.ID)
		}
	}
}

// Get retrieves the LATEST version of an artifact series.
func (as *ArtifactStore) Get(id ArtifactID) (*Artifact, bool) {
	as.mu.RLock()
	defer as.mu.RUnlock()

	history, ok := as.store[id]
	if !ok || len(history) == 0 {
		return nil, false
	}
	return cloneArtifact(history[len(history)-1]), true
}

// FindByID is an alias for Get for backward compatibility.
func (as *ArtifactStore) FindByID(id ArtifactID) (*Artifact, bool) {
	return as.Get(id)
}

// GetByExecution returns all artifacts created during a specific execution run
func (as *ArtifactStore) GetByExecution(execID string) []*Artifact {
	as.mu.RLock()
	defer as.mu.RUnlock()

	var results []*Artifact
	for _, history := range as.store {
		if len(history) > 0 {
			latest := history[len(history)-1]
			if latest.Execution == execID {
				results = append(results, cloneArtifact(latest))
			}
		}
	}
	return results
}

// GetVersion retrieves a specific version of an artifact series (1-indexed).
func (as *ArtifactStore) GetVersion(id ArtifactID, version uint32) (*Artifact, bool) {
	as.mu.RLock()
	defer as.mu.RUnlock()

	history, ok := as.store[id]
	if !ok || version == 0 || int(version) > len(history) {
		return nil, false
	}
	return cloneArtifact(history[version-1]), true
}

// GetAllVersions returns the full history slice of an artifact.
func (as *ArtifactStore) GetAllVersions(id ArtifactID) ([]*Artifact, bool) {
	as.mu.RLock()
	defer as.mu.RUnlock()

	history, ok := as.store[id]
	if !ok {
		return nil, false
	}
	// Return a copy to prevent race conditions on the slice
	copyHistory := make([]*Artifact, len(history))
	for i, a := range history {
		copyHistory[i] = cloneArtifact(a)
	}
	return copyHistory, true
}

// Exists checks if an artifact series exists.
func (as *ArtifactStore) Exists(id ArtifactID) bool {
	as.mu.RLock()
	defer as.mu.RUnlock()
	_, ok := as.store[id]
	return ok
}

// FindAll returns the latest version of ALL artifact series in the store.
func (as *ArtifactStore) FindAll() []*Artifact {
	as.mu.RLock()
	defer as.mu.RUnlock()

	var results []*Artifact
	for _, history := range as.store {
		if len(history) > 0 {
			results = append(results, cloneArtifact(history[len(history)-1]))
		}
	}
	return results
}

// FindByProducer returns the latest version of all artifacts created by a specific worker.
func (as *ArtifactStore) FindByProducer(producer string) []*Artifact {
	as.mu.RLock()
	defer as.mu.RUnlock()

	var results []*Artifact
	for _, id := range as.byProducer[producer] {
		if history, ok := as.store[id]; ok && len(history) > 0 {
			results = append(results, cloneArtifact(history[len(history)-1]))
		}
	}
	return results
}

// FindByType returns the latest version of all artifacts matching a specific type.
func (as *ArtifactStore) FindByType(t ArtifactType) []*Artifact {
	as.mu.RLock()
	defer as.mu.RUnlock()

	var results []*Artifact
	for _, id := range as.byType[t] {
		if history, ok := as.store[id]; ok && len(history) > 0 {
			results = append(results, cloneArtifact(history[len(history)-1]))
		}
	}
	return results
}

// FindByParent returns the latest version of the parent artifacts of a given artifact ID.
func (as *ArtifactStore) FindByParent(id ArtifactID) []*Artifact {
	as.mu.RLock()
	defer as.mu.RUnlock()

	var results []*Artifact
	if history, ok := as.store[id]; ok && len(history) > 0 {
		latest := history[len(history)-1]
		for _, pid := range latest.Parents {
			if parentHistory, ok := as.store[pid]; ok && len(parentHistory) > 0 {
				results = append(results, cloneArtifact(parentHistory[len(parentHistory)-1]))
			}
		}
	}
	return results
}

// FindChildren returns the latest version of all artifacts that declare the given artifact as a parent.
func (as *ArtifactStore) FindChildren(parentID ArtifactID) []*Artifact {
	as.mu.RLock()
	defer as.mu.RUnlock()

	var results []*Artifact
	for _, id := range as.byParent[parentID] {
		if history, ok := as.store[id]; ok && len(history) > 0 {
			results = append(results, cloneArtifact(history[len(history)-1]))
		}
	}
	return results
}

// Delete removes an artifact series from the store and its indexes.
func (as *ArtifactStore) Delete(id ArtifactID) {
	as.mu.Lock()
	defer as.mu.Unlock()

	history, ok := as.store[id]
	if !ok || len(history) == 0 {
		return
	}

	latest := history[len(history)-1]

	// Remove from main store
	delete(as.store, id)

	// Remove from byProducer
	as.byProducer[latest.Producer] = removeID(as.byProducer[latest.Producer], id)
	// Remove from byType
	as.byType[latest.Type] = removeID(as.byType[latest.Type], id)
	// Remove from byParent
	for _, pid := range latest.Parents {
		as.byParent[pid] = removeID(as.byParent[pid], id)
	}
}

func removeID(slice []ArtifactID, target ArtifactID) []ArtifactID {
	for i, id := range slice {
		if id == target {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

// UpdateVersion performs an append-only update on an artifact series.
// It clones the latest version, updates the data, and saves it as the next version.
func (as *ArtifactStore) UpdateVersion(id ArtifactID, newData any) (*Artifact, error) {
	// First, fetch the existing latest artifact (RLock)
	as.mu.RLock()
	history, ok := as.store[id]
	as.mu.RUnlock()

	if !ok || len(history) == 0 {
		return nil, fmt.Errorf("artifact %s not found", id)
	}

	latest := history[len(history)-1]

	// Create the new append-only artifact clone (ID remains the same)
	newArt := &Artifact{
		ID:        latest.ID,
		Name:      latest.Name,
		Type:      latest.Type,
		Producer:  latest.Producer,
		Workflow:  latest.Workflow,
		Execution: latest.Execution,
		Task:      latest.Task,
		Parents:   latest.Parents, // Keep the same parents for the series
		CreatedAt: latest.CreatedAt,
		Version:   latest.Version + 1, // Will be enforced by save() anyway
		Data:      newData,
	}

	// Save acquires the Lock and appends it to the slice
	as.save(newArt)

	return newArt, nil
}

func cloneArtifact(a *Artifact) *Artifact {
	if a == nil {
		return nil
	}
	b := *a
	b.Data = cloneValue(a.Data)
	b.Parents = append([]ArtifactID(nil), a.Parents...)
	if a.StoredAt != nil {
		t := *a.StoredAt
		b.StoredAt = &t
	}
	return &b
}
