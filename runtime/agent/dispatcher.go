package agent

import (
	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
)

// Dispatcher listens for TaskReady events and schedules them to the appropriate Worker.
type Dispatcher struct {
	Logger  *logger.Logger
	Bus     *events.Bus
	Workers map[WorkerID]*Worker
}

func NewDispatcher(l *logger.Logger, b *events.Bus) *Dispatcher {
	return &Dispatcher{
		Logger:  l,
		Bus:     b,
		Workers: make(map[WorkerID]*Worker),
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
