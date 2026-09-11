package memory

import (
	"fmt"
	"sync"
	"time"
)

// ArtifactStore maintains the retained artifact history and specialized indexes.
type ArtifactStore struct {
	store       map[ArtifactID][]*Artifact
	byProducer  map[string][]ArtifactID
	byType      map[ArtifactType][]ArtifactID
	byParent    map[ArtifactID][]ArtifactID
	mu          sync.RWMutex
	maxVersions int
}

func NewArtifactStore() *ArtifactStore {
	return NewArtifactStoreWithRetention(DefaultRetentionPolicy().MaxArtifactVersions)
}

func NewArtifactStoreWithRetention(maxVersions int) *ArtifactStore {
	if maxVersions <= 0 {
		maxVersions = DefaultRetentionPolicy().MaxArtifactVersions
	}
	return &ArtifactStore{
		store:       make(map[ArtifactID][]*Artifact),
		byProducer:  make(map[string][]ArtifactID),
		byType:      make(map[ArtifactType][]ArtifactID),
		byParent:    make(map[ArtifactID][]ArtifactID),
		maxVersions: maxVersions,
	}
}

// save physically writes the artifact, auto-increments version, and updates indexes.
func (as *ArtifactStore) save(artifact *Artifact) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.saveLocked(artifact)
}

// saveLocked requires exclusive ownership of mu.
func (as *ArtifactStore) saveLocked(artifact *Artifact) {
	if artifact.StoredAt == nil {
		now := time.Now()
		artifact.StoredAt = &now
	}

	history := as.store[artifact.ID]
	var previous *Artifact
	if len(history) > 0 {
		previous = history[len(history)-1]
	}

	// Keep logical versions monotonic even after old retained versions are pruned.
	artifact.Version = 1
	if len(history) > 0 {
		artifact.Version = history[len(history)-1].Version + 1
	}

	history = append(history, cloneArtifact(artifact))
	if len(history) > as.maxVersions {
		history = history[len(history)-as.maxVersions:]
	}
	as.store[artifact.ID] = history

	// If this is the first version, update secondary indexes
	if previous != nil {
		as.byProducer[previous.Producer] = removeID(as.byProducer[previous.Producer], artifact.ID)
		as.byType[previous.Type] = removeID(as.byType[previous.Type], artifact.ID)
		for _, p := range previous.Parents {
			as.byParent[p] = removeID(as.byParent[p], artifact.ID)
		}
	}
	as.byProducer[artifact.Producer] = append(as.byProducer[artifact.Producer], artifact.ID)
	as.byType[artifact.Type] = append(as.byType[artifact.Type], artifact.ID)

	for _, parentID := range artifact.Parents {
		as.byParent[parentID] = append(as.byParent[parentID], artifact.ID)
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
	if !ok || version == 0 {
		return nil, false
	}
	for _, artifact := range history {
		if artifact.Version == version {
			return cloneArtifact(artifact), true
		}
	}
	return nil, false
}

// Snapshot returns owned copies of retained artifact histories.
func (as *ArtifactStore) Snapshot() map[ArtifactID][]*Artifact {
	as.mu.RLock()
	defer as.mu.RUnlock()
	result := make(map[ArtifactID][]*Artifact, len(as.store))
	for id, history := range as.store {
		for _, artifact := range history {
			result[id] = append(result[id], cloneArtifact(artifact))
		}
	}
	return result
}

func (as *ArtifactStore) restoreSeries(id ArtifactID, history []*Artifact) error {
	as.mu.Lock()
	defer as.mu.Unlock()
	if current := as.store[id]; len(current) > 0 {
		latest := current[len(current)-1]
		as.byProducer[latest.Producer] = removeID(as.byProducer[latest.Producer], id)
		as.byType[latest.Type] = removeID(as.byType[latest.Type], id)
		for _, parent := range latest.Parents {
			as.byParent[parent] = removeID(as.byParent[parent], id)
		}
	}
	delete(as.store, id)
	for _, artifact := range history {
		if artifact == nil || artifact.ID != id {
			return fmt.Errorf("invalid artifact rollback")
		}
		as.store[id] = append(as.store[id], cloneArtifact(artifact))
	}
	if len(history) > 0 {
		latest := history[len(history)-1]
		as.byProducer[latest.Producer] = append(as.byProducer[latest.Producer], id)
		as.byType[latest.Type] = append(as.byType[latest.Type], id)
		for _, parent := range latest.Parents {
			as.byParent[parent] = append(as.byParent[parent], id)
		}
	}
	return nil
}

// Restore replaces the store and rebuilds all latest-version indexes.
func (as *ArtifactStore) Restore(snapshot map[ArtifactID][]*Artifact) error {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.store = make(map[ArtifactID][]*Artifact)
	as.byProducer = make(map[string][]ArtifactID)
	as.byType = make(map[ArtifactType][]ArtifactID)
	as.byParent = make(map[ArtifactID][]ArtifactID)
	for id, history := range snapshot {
		if id == "" || len(history) == 0 {
			return fmt.Errorf("invalid restored artifact history")
		}
		if len(history) > as.maxVersions {
			history = history[len(history)-as.maxVersions:]
		}
		for _, artifact := range history {
			if artifact == nil || artifact.ID != id {
				return fmt.Errorf("invalid restored artifact %s", id)
			}
			as.store[id] = append(as.store[id], cloneArtifact(artifact))
		}
		latest := as.store[id][len(as.store[id])-1]
		as.byProducer[latest.Producer] = append(as.byProducer[latest.Producer], id)
		as.byType[latest.Type] = append(as.byType[latest.Type], id)
		for _, parent := range latest.Parents {
			as.byParent[parent] = append(as.byParent[parent], id)
		}
	}
	return nil
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
	// Keep selection and append atomic with respect to saves and deletion.
	as.mu.Lock()
	defer as.mu.Unlock()
	history, ok := as.store[id]

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
		Version:   latest.Version + 1,
		Data:      newData,
	}

	as.saveLocked(newArt)

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
