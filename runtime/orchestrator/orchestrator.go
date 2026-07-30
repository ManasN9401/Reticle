package orchestrator

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
	"github.com/hyperparallel/runtime/memory"
)

type Orchestrator struct {
	Logger       *logger.Logger
	Bus          *events.Bus
	Artifacts    *memory.ArtifactManager
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
	artifacts := memory.NewArtifactManager()
	runtimeState := memory.NewRuntimeState()
	sessionState := memory.NewSessionState(sessionID)
	
	memory.NewManager(artifacts, runtimeState, sessionState, b)

	// The central Event Logger (Source of Truth)
	b.SubscribeAll(func(e events.RuntimeEvent) {
		l.Info(
			"Runtime Event",
			"event", e.Type,
			"id", e.ID, 
			"session", e.SessionID,
			"component", e.Source, 
			"payload", e.Payload,
		)
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
	o.Logger.Info("HyperParallel Skeleton Runtime Orchestrator started", "session", o.Bus.SessionID())
	o.Bus.Publish(events.EventType("RuntimeStarted"), events.Component("orchestrator"), nil)
}

func (o *Orchestrator) Shutdown() {
	o.Logger.Info("HyperParallel Skeleton Runtime Orchestrator shutting down", "session", o.Bus.SessionID())
	o.Bus.Publish(events.EventType("RuntimeShutdown"), events.Component("orchestrator"), nil)
}
