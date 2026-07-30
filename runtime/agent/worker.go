package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
	"github.com/hyperparallel/runtime/memory"
)

type WorkerID string
type TaskID string

// TaskRequest represents what a worker is asked to do.
type TaskRequest struct {
	ID        TaskID `json:"id"`
	SessionID string `json:"session_id,omitempty"`
	Workflow  string `json:"workflow,omitempty"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
}

// TaskResponse represents what a worker returns.
type TaskResponse struct {
	ID       TaskID           `json:"id"`
	Result   string           `json:"result,omitempty"`   // Legacy scalar result
	Artifact *memory.Artifact `json:"artifact,omitempty"` // Structured artifact result
	Error    string           `json:"error,omitempty"`
}

type Worker struct {
	ID         WorkerID
	Executable string
	Args       []string
	Logger     *logger.Logger
	Bus        *events.Bus
}

func NewWorker(id WorkerID, executable string, args []string, l *logger.Logger, b *events.Bus) *Worker {
	return &Worker{
		ID:         id,
		Executable: executable,
		Args:       args,
		Logger:     l,
		Bus:        b,
	}
}

func (w *Worker) Execute(req TaskRequest) (*TaskResponse, error) {
	w.Bus.Publish(events.EventType("WorkerStarted"), events.Component("worker"), map[string]any{
		"task_id":   req.ID,
		"worker_id": w.ID,
	})

	cmd := exec.Command(w.Executable, w.Args...)
	
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Send request as JSON
	reqJSON, _ := json.Marshal(req)
	fmt.Fprintf(stdin, "%s\n", reqJSON)
	stdin.Close() // Signal EOF

	// Read response
	scanner := bufio.NewScanner(stdout)
	var resp TaskResponse
	if scanner.Scan() {
		line := scanner.Text()
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			return nil, fmt.Errorf("failed to parse worker output: %v, raw: %s", err, line)
		}
	} else {
		return nil, fmt.Errorf("worker produced no output")
	}

	if err := cmd.Wait(); err != nil {
		w.Logger.Error("Worker exited with error", "worker_id", w.ID, "error", err)
	}

	w.Bus.Publish(events.EventType("WorkerCompleted"), events.Component("worker"), map[string]any{
		"task_id":   req.ID,
		"worker_id": w.ID,
	})
	return &resp, nil
}
