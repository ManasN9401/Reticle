package orchestrator

import (
	"crypto/rand"
	"encoding/hex"
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
	artifacts := memory.NewArtifactStore()
	runtimeState := memory.NewRuntimeState()
	sessionState := memory.NewSessionState(sessionID)
	
	memory.NewManager(artifacts, runtimeState, sessionState, b)

	// The central Event Logger (Source of Truth)
	b.SubscribeAll(func(e events.RuntimeEvent) {
		payload := e.Payload

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

func (o *Orchestrator) Start() {
	// Infrastructural log, not a domain event
	o.Logger.Info("Reticle Skeleton Runtime Orchestrator started", "session", o.Bus.SessionID())
	o.Bus.Publish(events.EventType("RuntimeStarted"), events.Component("orchestrator"), nil)
}

func (o *Orchestrator) Shutdown() {
	o.Logger.Info("Reticle Skeleton Runtime Orchestrator shutting down", "session", o.Bus.SessionID())
	o.Bus.Publish(events.EventType("RuntimeShutdown"), events.Component("orchestrator"), nil)
}
