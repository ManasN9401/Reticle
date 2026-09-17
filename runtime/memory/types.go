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

// MemoryScope defines the boundary for how far memory is shared.
type MemoryScope string

const (
	ScopeGlobal    MemoryScope = "global"
	ScopeWorkflow  MemoryScope = "workflow"
	ScopeExecution MemoryScope = "execution"
	ScopeAgent     MemoryScope = "agent"
)

// MemoryEntry represents a rich value stored in the Shared Runtime Memory.
type MemoryEntry struct {
	Key     string      `json:"key"`
	Value   any         `json:"value"`
	Scope   MemoryScope `json:"scope"`
	ScopeID string      `json:"scope_id"` // Matches the scope (e.g., ExecutionID or AgentID)
	Owner   string      `json:"owner"`    // Who produced this memory
	Version uint64      `json:"version"`
	// ExpectedVersion enables optimistic concurrency. Nil is an unconditional
	// write; zero means the key must not exist.
	ExpectedVersion *uint64    `json:"expected_version,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

// MemoryWriteRequest lets a worker wait until its mutation has been accepted
// or rejected. Result must be buffered because the requester can be cancelled.
type MemoryWriteRequest struct {
	Entry  MemoryEntry
	Result chan error
}

type ArtifactWriteRequest struct {
	Artifact *Artifact
	Result   chan error
}

// ResultCommitRequest atomically persists one worker's memory mutations and
// optional artifact before the worker completion event can be published.
type ResultCommitRequest struct {
	AttemptID string
	Entries   []MemoryEntry
	Artifact  *Artifact
	Result    chan error
}

// RetentionPolicy bounds ephemeral state. Zero values use safe defaults.
type RetentionPolicy struct {
	MaxEntries          int
	MaxBytes            int64
	ExecutionTTL        time.Duration
	MaxArtifactVersions int
}

func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		MaxEntries:          10_000,
		MaxBytes:            16 << 20,
		ExecutionTTL:        24 * time.Hour,
		MaxArtifactVersions: 100,
	}
}
