package memory

import "time"

// MemoryKey is a strongly typed identifier for simple runtime state entries.
type MemoryKey string

// ArtifactID uniquely identifies an artifact in the system.
type ArtifactID string

// ArtifactType categorizes the payload (e.g., "document/markdown", "data/json").
type ArtifactType string

// Artifact represents a standard unit of work/state passed between agents.
type Artifact struct {
	ID        ArtifactID   `json:"id"`
	Name      string       `json:"name"`
	Type      ArtifactType `json:"type"`
	Producer  string       `json:"producer"` // Using string to avoid import cycles with agent.WorkerID
	Workflow  string       `json:"workflow,omitempty"`
	Execution string       `json:"execution,omitempty"`
	Task      string       `json:"task,omitempty"`
	Parents   []ArtifactID `json:"parents,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	StoredAt  *time.Time   `json:"stored_at,omitempty"`
	Version   uint32       `json:"version"`
	Data      any          `json:"data,omitempty"`
}
