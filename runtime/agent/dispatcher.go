package agent

import (
	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
	"github.com/hyperparallel/runtime/routing"
)

// Dispatcher listens for TaskReady events and schedules them to the appropriate Worker.
type Dispatcher struct {
	Logger       *logger.Logger
	Bus          *events.Bus
	Workers      map[WorkerID]*Worker
	Instructions *InstructionStore
	Router       *routing.ModelRouter
}

func NewDispatcher(l *logger.Logger, b *events.Bus, is *InstructionStore, r *routing.ModelRouter) *Dispatcher {
	return &Dispatcher{
		Logger:       l,
		Bus:          b,
		Workers:      make(map[WorkerID]*Worker),
		Instructions: is,
		Router:       r,
	}
}

func (d *Dispatcher) RegisterWorker(w *Worker) {
	d.Workers[w.ID] = w
}

func (d *Dispatcher) Start() {
	d.Bus.Subscribe(events.EventType("TaskCreated"), func(e events.RuntimeEvent) {
		task, ok := e.Payload.(Task)
		if !ok {
			d.Logger.Error("Dispatcher received invalid TaskCreated payload")
			return
		}

		workerID := WorkerID(task.AgentID)
		worker, ok := d.Workers[workerID]
		if !ok {
			d.Logger.Error("Dispatcher failed to trigger worker", "worker_id", workerID, "error", "worker not found")
			return
		}

		// Inject instructions dynamically
		if d.Instructions != nil {
			task.Instructions = d.Instructions.GetForTask(task.AgentID, task.Workflow)
		}

		// Inject Model Routing
		if d.Router != nil {
			if task.Parameters == nil {
				task.Parameters = make(map[string]any)
			}
			if _, exists := task.Parameters["llm_model"]; !exists {
				selectedModel := d.Router.SelectModel(string(task.ID), task.AgentID, 0.90) // 90% confidence threshold
				task.Parameters["llm_model"] = selectedModel
			} else {
				// Tell the router to track this forced model so it can learn from it
				d.Router.TrackForcedModel(string(task.ID), task.Parameters["llm_model"].(string))
			}
		}

		// Asynchronously invoke the worker directly
		go func(w *Worker, t Task) {
			d.Bus.Publish(events.EventType("TaskDispatched"), events.Component("dispatcher"), map[string]any{
				"task_id":   t.ID,
				"worker_id": w.ID,
			})
			
			_, failure := w.Execute(t)
			if failure != nil {
				d.Logger.Error("Worker execution failed", "worker_id", w.ID, "reason", failure.Reason, "stderr", failure.Stderr)
				
				d.Bus.Publish(events.EventType("WorkerFailed"), events.Component("dispatcher"), map[string]any{
					"task_id":   t.ID,
					"worker_id": w.ID,
					"reason":    failure.Reason,
					"exit_code": failure.ExitCode,
					"stderr":    failure.Stderr,
				})
				return
			}

		}(worker, task)
	})
}
