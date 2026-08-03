package agent



// Subscription binds a worker to a specific trigger condition via structured filters.
type Subscription struct {
	ID        string
	WorkerID  WorkerID
	EventType string
	Filters   map[string]string
}
