package agent

import "github.com/hyperparallel/runtime/events"

// Subscription binds a worker to a specific trigger condition.
type Subscription struct {
	ID        string
	WorkerID  WorkerID
	EventType string
	
	// Condition evaluates the event payload to see if the worker cares.
	Condition func(e events.RuntimeEvent) bool
	
	// Generate dynamically creates the Task to pass to the Worker based on the triggering event.
	Generate func(e events.RuntimeEvent) Task
}
