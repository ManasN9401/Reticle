package agent

import (
	"fmt"
	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
	"github.com/hyperparallel/runtime/memory"
)

type Supervisor struct {
	Logger *logger.Logger
	Bus    *events.Bus
}

func NewSupervisor(l *logger.Logger, b *events.Bus) *Supervisor {
	return &Supervisor{
		Logger: l,
		Bus:    b,
	}
}

func (s *Supervisor) AssignTask(worker *Worker, req TaskRequest, memoryKey memory.MemoryKey) error {
	// The event is the single source of truth for the domain action.
	// The central event logger will automatically pick this up.
	s.Bus.Publish(events.EventType("SupervisorTaskAssigned"), events.Component("supervisor"), map[string]any{
		"task_id":   req.ID,
		"worker_id": worker.ID,
	})
	
	resp, err := worker.Execute(req)
	if err != nil {
		s.Logger.Error("Supervisor caught error from worker", "worker_id", worker.ID, "error", err)
		return err
	}

	if resp.Error != "" {
		s.Logger.Error("Worker reported error", "worker_id", worker.ID, "error", resp.Error)
		return fmt.Errorf(resp.Error)
	}

	if resp.Artifact != nil {
		s.Bus.Publish(events.EventType("ArtifactStoreRequested"), events.Component("supervisor"), resp.Artifact)
	} else if resp.Result != "" {
		// Strictly event-driven. The supervisor CANNOT touch memory directly.
		s.Bus.Publish(events.EventType("MemoryUpdateRequested"), events.Component("supervisor"), map[string]any{
			"key":   memoryKey,
			"value": resp.Result,
		})
	}
	
	return nil
}
