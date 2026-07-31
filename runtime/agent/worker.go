package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/hyperparallel/runtime/events"
	"github.com/hyperparallel/runtime/logger"
	"github.com/hyperparallel/runtime/memory"
)

type WorkerID string
type TaskID string

// TaskInput represents structured data given to a worker.
type TaskInput struct {
	ArtifactID string `json:"artifact_id,omitempty"`
	Version    int    `json:"version,omitempty"`
	Name       string `json:"name,omitempty"`
	Data       any    `json:"data,omitempty"`
}

type WorkerFailureReason string

const (
	WorkerExitedNonZero   WorkerFailureReason = "exit_non_zero"
	WorkerProtocolError   WorkerFailureReason = "protocol_error"
	WorkerInvalidJSON     WorkerFailureReason = "invalid_json"
	WorkerTimeout         WorkerFailureReason = "timeout"
	WorkerPanic           WorkerFailureReason = "panic"
	WorkerStartFailed     WorkerFailureReason = "start_failed"
)

type WorkerFailure struct {
	Reason   WorkerFailureReason `json:"reason"`
	ExitCode int                 `json:"exit_code"`
	Stderr   string              `json:"stderr"`
}

type Task struct {
	ID          TaskID         `json:"id"`
	AgentID     string         `json:"agent_id,omitempty"`
	ExecutionID string         `json:"execution,omitempty"`
	Workflow    string         `json:"workflow,omitempty"`
	Inputs      []TaskInput    `json:"inputs,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type TaskResponse struct {
	ID       TaskID           `json:"id"`
	Result   string           `json:"result,omitempty"`   // Legacy scalar result
	Artifact *memory.Artifact `json:"artifact,omitempty"` // Structured artifact result
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

func (w *Worker) Execute(req Task) (*TaskResponse, *WorkerFailure) {
	w.Bus.Publish(events.EventType("WorkerStarted"), events.Component("worker"), map[string]any{
		"task_id":   req.ID,
		"worker_id": w.ID,
	})

	cmd := exec.Command(w.Executable, w.Args...)
	
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, &WorkerFailure{Reason: WorkerStartFailed, ExitCode: -1, Stderr: err.Error()}
	}
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &WorkerFailure{Reason: WorkerStartFailed, ExitCode: -1, Stderr: err.Error()}
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, &WorkerFailure{Reason: WorkerStartFailed, ExitCode: -1, Stderr: err.Error()}
	}

	// Send request as JSON
	reqJSON, _ := json.Marshal(req)
	fmt.Fprintf(stdin, "%s\n", reqJSON)
	stdin.Close() // Signal EOF

	// Read response
	scanner := bufio.NewScanner(stdout)
	var resp TaskResponse
	var parseErr error

	if scanner.Scan() {
		line := scanner.Text()
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			parseErr = fmt.Errorf("failed to parse worker output: %v, raw: %s", err, line)
		}
	} else {
		parseErr = fmt.Errorf("worker produced no output")
	}

	err = cmd.Wait()
	
	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		
		return nil, &WorkerFailure{
			Reason:   WorkerExitedNonZero,
			ExitCode: exitCode,
			Stderr:   stderrBuf.String(),
		}
	}

	if parseErr != nil {
		return nil, &WorkerFailure{
			Reason:   WorkerInvalidJSON,
			ExitCode: 0,
			Stderr:   parseErr.Error(),
		}
	}

	// Auto-hydrate the artifact with orchestrator context
	if resp.Artifact != nil {
		resp.Artifact.Producer = string(w.ID)
		resp.Artifact.Task = string(req.ID)
		resp.Artifact.Workflow = req.Workflow
		resp.Artifact.Execution = req.ExecutionID
		resp.Artifact.CreatedAt = time.Now()
		
		for _, input := range req.Inputs {
			resp.Artifact.Parents = append(resp.Artifact.Parents, memory.ArtifactID(input.ArtifactID))
		}
	}

	w.Bus.Publish(events.EventType("WorkerCompleted"), events.Component("worker"), map[string]any{
		"task_id":   req.ID,
		"worker_id": w.ID,
	})

	if resp.Artifact != nil {
		w.Bus.Publish(events.EventType("ArtifactsProduced"), events.Component("worker"), resp.Artifact)
	} else if resp.Result != "" {
		w.Bus.Publish(events.EventType("MemoryUpdateRequested"), events.Component("worker"), map[string]any{
			"key":   req.ID,
			"value": resp.Result,
		})
	}

	return &resp, nil
}
