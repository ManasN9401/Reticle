package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
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
	Origin      string         `json:"origin,omitempty"`
	Inputs       []TaskInput    `json:"inputs,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty"`
	Memory       map[string]any `json:"memory,omitempty"`
	Instructions []string       `json:"instructions,omitempty"`
}

type GraphMutation struct {
	Action             string `json:"action"`
	TargetAgent        string `json:"target_agent"`
	ReturnToSupervisor bool   `json:"return_to_supervisor"`
}

type TaskResponse struct {
	ID            TaskID           `json:"id"`
	Result        string           `json:"result,omitempty"`   // Legacy scalar result
	Artifact      *memory.Artifact `json:"artifact,omitempty"` // Structured artifact result
	GraphMutation *GraphMutation   `json:"graph_mutation,omitempty"`
}

type Worker struct {
	ID         WorkerID
	Executable string
	Args       []string
	EnvVars    []string
	Logger     *logger.Logger
	Bus        *events.Bus
}

func NewWorker(id WorkerID, executable string, args []string, envVars []string, l *logger.Logger, b *events.Bus) *Worker {
	return &Worker{
		ID:         id,
		Executable: executable,
		Args:       args,
		EnvVars:    envVars,
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
	
	if len(w.EnvVars) > 0 {
		cmd.Env = append(os.Environ(), w.EnvVars...)
	}
	
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
	var foundJson bool
	var lastRawLine string

	for scanner.Scan() {
		line := scanner.Text()
		lineStr := string(line)
		if lineStr != "" {
			lastRawLine = lineStr
			// Try parsing; we only care if we get at least one valid json
			if err := json.Unmarshal([]byte(line), &resp); err == nil {
				foundJson = true
				parseErr = nil
			} else {
				parseErr = fmt.Errorf("failed to parse worker output: %v, raw: %s", err, line)
			}
		}
	}

	if !foundJson {
		if parseErr == nil {
			parseErr = fmt.Errorf("worker produced no valid json output, last raw output: %s", lastRawLine)
		}
	}

	err = cmd.Wait()
	
	if stderrBuf.Len() > 0 {
		w.Logger.Info("Worker emitted stderr", "worker_id", w.ID, "stderr", stderrBuf.String())
	}
	
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
		entry := memory.MemoryEntry{
			Key:     string(req.ID),
			Value:   resp.Result,
			Scope:   memory.ScopeExecution, // Defaulting scalar results to Execution scope
			ScopeID: req.ExecutionID,
			Owner:   req.AgentID,
		}
		w.Bus.Publish(events.EventType("MemoryWriteRequested"), events.Component("worker"), entry)
	}

	if resp.GraphMutation != nil {
		w.Bus.Publish(events.EventType("GraphMutationRequested"), events.Component("worker"), map[string]any{
			"task_id":   req.ID,
			"worker_id": w.ID,
			"execution": req.ExecutionID,
			"workflow":  req.Workflow,
			"mutation":  resp.GraphMutation,
		})
	}

	return &resp, nil
}
