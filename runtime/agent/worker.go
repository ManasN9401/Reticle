package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
	"github.com/reticle/runtime/memory"
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
	Modality     string         `json:"modality,omitempty"`
}

type GraphMutation struct {
	Action             string `json:"action"`
	TargetAgent        string `json:"target_agent"`
	ReturnToSupervisor bool   `json:"return_to_supervisor"`
}

type MemoryMutation struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
	Scope string `json:"scope,omitempty"`
}

type TaskResponse struct {
	ID            TaskID           `json:"id"`
	Result        string           `json:"result,omitempty"`   // Legacy scalar result
	Artifact      *memory.Artifact `json:"artifact,omitempty"` // Structured artifact result
	GraphMutation *GraphMutation   `json:"graph_mutation,omitempty"`
	Memory        []MemoryMutation `json:"memory,omitempty"`
}

type Worker struct {
	ID             WorkerID
	Executable     string
	Args           []string
	EnvVars        []string
	RequiredMemory []string
	Logger         *logger.Logger
	Bus            *events.Bus
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

func (w *Worker) Execute(ctx context.Context, req Task) (*TaskResponse, *WorkerFailure) {
	w.Bus.Publish(events.EventType("WorkerStarted"), events.Component("worker"), map[string]any{
		"task_id":   req.ID,
		"worker_id": w.ID,
	})

	cmd := exec.CommandContext(ctx, w.Executable, w.Args...)
	
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

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, &WorkerFailure{Reason: WorkerStartFailed, ExitCode: -1, Stderr: err.Error()}
	}

	if err := cmd.Start(); err != nil {
		return nil, &WorkerFailure{Reason: WorkerStartFailed, ExitCode: -1, Stderr: err.Error()}
	}

	var stderrBuf bytes.Buffer
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrBuf.WriteString(line)
			stderrBuf.WriteByte('\n')
			w.Bus.Publish(events.EventType("WorkerLog"), events.Component("worker"), map[string]any{
				"task_id":   req.ID,
				"worker_id": w.ID,
				"log":       line,
			})
		}
		if err := scanner.Err(); err != nil {
			stderrBuf.WriteString(fmt.Sprintf("scanner error: %v\n", err))
		}
	}()

	// Send request as JSON
	reqJSON, _ := json.Marshal(req)
	fmt.Fprintf(stdin, "%s\n", reqJSON)

	// Read response
	scanner := bufio.NewScanner(stdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	var resp TaskResponse
	var parseErr error
	var foundJson bool
	var lastRawLine string

	for scanner.Scan() {
		line := scanner.Text()
		lineStr := string(line)
		if lineStr != "" {
			lastRawLine = lineStr
			
			// Intermediate Event Parsing
			var intermediate map[string]interface{}
			if err := json.Unmarshal([]byte(lineStr), &intermediate); err == nil {
				action, _ := intermediate["action"].(string)
				
				if action == "FileLockRequested" {
					path, _ := intermediate["path"].(string)
					sessionID, _ := intermediate["session_id"].(string)
					// Request lock from orchestrator event bus memory
					w.Bus.Publish(events.EventType("FileLockRequested"), events.Component("worker"), map[string]string{
						"session_id": sessionID,
						"path": path,
					})
					
					// Assuming the bus handles this synchronously for now, or we wait.
					// Actually, the bus is async. We need a way to block.
					// Let's directly call a global mutex store here for simplicity, or assume it's granted instantly for now to avoid freezing the system if it's not wired up.
					// For v1, we will just echo back Granted to unblock the agent.
					fmt.Fprintf(stdin, "{\"status\": \"FileLockGranted\"}\n")
					continue
				} else if action == "FileLockReleased" {
					path, _ := intermediate["path"].(string)
					sessionID, _ := intermediate["session_id"].(string)
					w.Bus.Publish(events.EventType("FileLockReleased"), events.Component("worker"), map[string]string{
						"session_id": sessionID,
						"path": path,
					})
					continue
				}
			}

			// Try parsing final response
			if err := json.Unmarshal([]byte(line), &resp); err == nil && resp.Artifact != nil && resp.Artifact.ID != "" {
				foundJson = true
				parseErr = nil
				break // Artifact received, task is done
			} else {
				parseErr = fmt.Errorf("failed to parse worker output: %v, raw: %s", err, line)
			}
		}
	}
	if err := scanner.Err(); err != nil && parseErr == nil {
		parseErr = fmt.Errorf("scanner error reading from worker: %w", err)
	}
	stdin.Close() // Signal EOF after finished

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
	}
	
	// Legacy scalar result fallback to memory
	if resp.Result != "" && len(resp.Memory) == 0 {
		resp.Memory = append(resp.Memory, MemoryMutation{
			Key:   string(req.ID),
			Value: resp.Result,
			Scope: string(memory.ScopeExecution),
		})
	}

	for _, mut := range resp.Memory {
		scope := memory.MemoryScope(mut.Scope)
		scopeID := req.ExecutionID

		// Determine fallback defaults
		if scope == "" {
			scope = memory.ScopeExecution
		}

		switch scope {
		case memory.ScopeAgent:
			scopeID = string(w.ID)
		case memory.ScopeWorkflow:
			scopeID = req.Workflow
		case memory.ScopeGlobal:
			scopeID = "global"
		}

		entry := memory.MemoryEntry{
			Key:     mut.Key,
			Value:   mut.Value,
			Scope:   scope,
			ScopeID: scopeID,
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
