package orchestrator

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
)

type Orchestrator struct {
	Logger       *logger.Logger
	Bus          *events.Bus
	Artifacts    *memory.ArtifactStore
	RuntimeState *memory.RuntimeState
	SessionState *memory.SessionState
}

func generateSessionID() string {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err == nil {
		return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(bytes)
	}
	return time.Now().Format("20060102-150405") // fallback
}

func New() *Orchestrator {
	l := logger.New()
	sessionID := generateSessionID()
	b := events.NewBus(sessionID)
	policy := memory.DefaultRetentionPolicy()
	policy.MaxEntries = positiveEnv("RETICLE_MEMORY_MAX_ENTRIES", policy.MaxEntries)
	policy.MaxBytes = int64(positiveEnv("RETICLE_MEMORY_MAX_MIB", int(policy.MaxBytes>>20))) << 20
	policy.MaxArtifactVersions = positiveEnv("RETICLE_ARTIFACT_MAX_VERSIONS", policy.MaxArtifactVersions)
	policy.ExecutionTTL = time.Duration(positiveEnv("RETICLE_EXECUTION_MEMORY_TTL_HOURS", int(policy.ExecutionTTL/time.Hour))) * time.Hour
	artifacts := memory.NewArtifactStoreWithRetention(policy.MaxArtifactVersions)
	runtimeState := memory.NewRuntimeStateWithPolicy(policy)
	sessionState := memory.NewSessionState(sessionID)

	root := os.Getenv("RETICLE_ROOT")
	if root != "" && os.Getenv("RETICLE_MEMORY_PERSISTENCE") != "false" {
		path := filepath.Join(root, ".reticle", "memory", "state.json")
		if _, err := memory.NewPersistentManager(artifacts, runtimeState, sessionState, b, path); err != nil {
			l.Error("Memory restart recovery disabled", "error", err)
			memory.NewManager(artifacts, runtimeState, sessionState, b)
		}
	} else {
		memory.NewManager(artifacts, runtimeState, sessionState, b)
	}

	// The central Event Logger (Source of Truth)
	b.SubscribeAll(func(e events.RuntimeEvent) {
		payload := e.Payload
		switch e.Type {
		case "TaskCreated", "MemoryWriteRequested", "MemoryReadCompleted", "ArtifactWriteRequested", "TaskResultCommitRequested", "WorkerLog":
			payload = map[string]any{"redacted": true}
		}

		// If it's an Artifact, strip the large Data payload unless DebugMode is explicitly on.
		if art, ok := payload.(*memory.Artifact); ok && !l.DebugMode {
			payload = map[string]any{
				"id":         art.ID,
				"version":    art.Version,
				"producer":   art.Producer,
				"workflow":   art.Workflow,
				"execution":  art.Execution,
				"created_at": art.CreatedAt,
			}
		}

		// Log structured event to the console and file
		l.Info("Runtime Event", "component", string(e.Source), "event", string(e.Type), "payload", payload)
	})

	return &Orchestrator{
		Logger:       l,
		Bus:          b,
		Artifacts:    artifacts,
		RuntimeState: runtimeState,
		SessionState: sessionState,
	}
}

func positiveEnv(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (o *Orchestrator) Start() {
	// Infrastructural log, not a domain event
	o.Logger.Info("Reticle Skeleton Runtime Orchestrator started", "session", o.Bus.SessionID())
	o.Bus.Publish(events.EventType("RuntimeStarted"), events.Component("orchestrator"), nil)
}

func (o *Orchestrator) Shutdown() {
	o.Logger.Info("Reticle Skeleton Runtime Orchestrator shutting down", "session", o.Bus.SessionID())
	o.Bus.Publish(events.EventType("RuntimeShutdown"), events.Component("orchestrator"), nil)
	o.Bus.Close()
	o.Logger.Close()
}
