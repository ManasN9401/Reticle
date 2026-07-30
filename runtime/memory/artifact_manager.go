package memory

import (
	"sync"
	"time"
)

// ArtifactManager maintains the in-memory knowledge graph and specialized indexes.
type ArtifactManager struct {
	store      map[ArtifactID]*Artifact
	byProducer map[string][]ArtifactID
	byType     map[ArtifactType][]ArtifactID
	byParent   map[ArtifactID][]ArtifactID
	mu         sync.RWMutex
}

func NewArtifactManager() *ArtifactManager {
	return &ArtifactManager{
		store:      make(map[ArtifactID]*Artifact),
		byProducer: make(map[string][]ArtifactID),
		byType:     make(map[ArtifactType][]ArtifactID),
		byParent:   make(map[ArtifactID][]ArtifactID),
	}
}

// save physically writes the artifact and updates all secondary indexes.
func (am *ArtifactManager) save(artifact *Artifact) {
	am.mu.Lock()
	defer am.mu.Unlock()
	
	if artifact.StoredAt.IsZero() {
		artifact.StoredAt = time.Now()
	}

	am.store[artifact.ID] = artifact

	// Update indexes
	am.byProducer[artifact.Producer] = append(am.byProducer[artifact.Producer], artifact.ID)
	am.byType[artifact.Type] = append(am.byType[artifact.Type], artifact.ID)
	
	for _, parentID := range artifact.Parents {
		am.byParent[parentID] = append(am.byParent[parentID], artifact.ID)
	}
}

// FindByID retrieves an artifact directly.
func (am *ArtifactManager) FindByID(id ArtifactID) (*Artifact, bool) {
	am.mu.RLock()
	defer am.mu.RUnlock()
	art, ok := am.store[id]
	return art, ok
}

// FindByProducer returns all artifacts created by a specific worker.
func (am *ArtifactManager) FindByProducer(producer string) []*Artifact {
	am.mu.RLock()
	defer am.mu.RUnlock()
	
	var results []*Artifact
	for _, id := range am.byProducer[producer] {
		if art, ok := am.store[id]; ok {
			results = append(results, art)
		}
	}
	return results
}
