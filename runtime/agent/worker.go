package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	WorkerExitedNonZero WorkerFailureReason = "exit_non_zero"
	WorkerProtocolError WorkerFailureReason = "protocol_error"
	WorkerInvalidJSON   WorkerFailureReason = "invalid_json"
	WorkerTimeout       WorkerFailureReason = "timeout"
	WorkerPanic         WorkerFailureReason = "panic"
	WorkerStartFailed   WorkerFailureReason = "start_failed"
)

type WorkerFailure struct {
	Reason   WorkerFailureReason `json:"reason"`
	ExitCode int                 `json:"exit_code"`
	Stderr   string              `json:"stderr"`
}

type Task struct {
	ID             TaskID                     `json:"id"`
	AgentID        string                     `json:"agent_id,omitempty"`
	ExecutionID    string                     `json:"execution,omitempty"`
	Workflow       string                     `json:"workflow,omitempty"`
	Origin         string                     `json:"origin,omitempty"`
	Inputs         []TaskInput                `json:"inputs,omitempty"`
	Parameters     map[string]any             `json:"parameters,omitempty"`
	Memory         map[string]any             `json:"memory,omitempty"`
	MemoryMetadata map[string]MemoryReference `json:"memory_metadata,omitempty"`
	Instructions   []string                   `json:"instructions,omitempty"`
	Modality       string                     `json:"modality,omitempty"`
}

type MemoryReference struct {
	Scope   memory.MemoryScope `json:"scope"`
	ScopeID string             `json:"scope_id"`
	Version uint64             `json:"version"`
}

type GraphMutation struct {
	Action             string `json:"action"`
	TargetAgent        string `json:"target_agent"`
	ReturnToSupervisor bool   `json:"return_to_supervisor"`
}

type MemoryMutation struct {
	Key             string  `json:"key"`
	Value           any     `json:"value"`
	Scope           string  `json:"scope,omitempty"`
	ExpectedVersion *uint64 `json:"expected_version,omitempty"`
}

type TaskResponse struct {
	ID            TaskID           `json:"id"`
	Result        string           `json:"result,omitempty"`   // Legacy scalar result
	Artifact      *memory.Artifact `json:"artifact,omitempty"` // Structured artifact result
	GraphMutation *GraphMutation   `json:"graph_mutation,omitempty"`
	Memory        []MemoryMutation `json:"memory,omitempty"`
}

type Worker struct {
	Prepare        func(context.Context) (string, []string, error)
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

// Execute implements one EOF-terminated request and one final JSON response.
func (w *Worker) Execute(ctx context.Context, req Task) (*TaskResponse, *WorkerFailure) {
	fail := func(reason WorkerFailureReason, err error) (*TaskResponse, *WorkerFailure) {
		return nil, &WorkerFailure{Reason: reason, ExitCode: -1, Stderr: err.Error()}
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fail(WorkerProtocolError, err)
	}
	w.Bus.Publish("WorkerStarted", "worker", map[string]any{"task_id": string(req.ID), "worker_id": string(w.ID)})
	if w.ID == "hitl-agent" {
		err := AwaitApproval(ctx, os.Getenv("RETICLE_ROOT"), req, func(line string) {
			w.Bus.Publish("WorkerLog", "worker", map[string]any{"task_id": string(req.ID), "worker_id": string(w.ID), "log": line})
		})
		if err != nil {
			return fail(WorkerProtocolError, err)
		}
		w.Bus.Publish("WorkerCompleted", "worker", map[string]any{"task_id": string(req.ID), "worker_id": string(w.ID)})
		return &TaskResponse{ID: req.ID, Result: "Approved"}, nil
	}
	executable, explicit := w.Executable, append([]string(nil), w.EnvVars...)
	if w.Prepare != nil {
		path, injected, prepareErr := w.Prepare(ctx)
		if prepareErr != nil {
			return fail(WorkerStartFailed, prepareErr)
		}
		executable = path
		explicit = append(explicit, injected...)
	}
	cmd := exec.CommandContext(ctx, executable, w.Args...)
	configureProcess(cmd)
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = workerEnvironment(req, explicit)
	out := &limitedOutput{limit: 10 * 1024 * 1024}
	errout := &limitedOutput{limit: 256 * 1024, onChunk: func(p []byte) {
		for _, line := range strings.Split(string(p), "\n") {
			if line != "" {
				w.Bus.Publish("WorkerLog", "worker", map[string]any{"task_id": string(req.ID), "worker_id": string(w.ID), "log": logger.Redact(line)})
			}
		}
	}}
	cmd.Stdout = out
	cmd.Stderr = errout
	err = cmd.Run()
	if ctx.Err() != nil {
		reason := WorkerTimeout
		if errors.Is(ctx.Err(), context.Canceled) {
			reason = "killed"
		}
		return fail(reason, ctx.Err())
	}
	if err != nil {
		code := -1
		reason := WorkerStartFailed
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
			reason = WorkerExitedNonZero
		}
		return nil, &WorkerFailure{Reason: reason, ExitCode: code, Stderr: logger.Redact(errout.String())}
	}
	if out.exceeded {
		return fail(WorkerProtocolError, fmt.Errorf("worker output exceeds 10 MiB"))
	}
	var resp TaskResponse
	found := false
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var candidate TaskResponse
		if json.Unmarshal([]byte(line), &candidate) != nil || candidate.ID == "" {
			continue
		}
		if found {
			return fail(WorkerProtocolError, fmt.Errorf("multiple final responses"))
		}
		resp = candidate
		found = true
	}
	if !found {
		return fail(WorkerInvalidJSON, fmt.Errorf("worker produced no final response"))
	}
	if resp.ID != req.ID {
		return fail(WorkerProtocolError, fmt.Errorf("response task identity mismatch"))
	}
	if resp.Artifact != nil && (resp.Artifact.ID == "" || resp.Artifact.Name == "" || resp.Artifact.Type == "") {
		return fail(WorkerProtocolError, fmt.Errorf("artifact requires id, name and type"))
	}
	if resp.Result != "" && len(resp.Memory) == 0 {
		resp.Memory = append(resp.Memory, MemoryMutation{Key: string(req.ID), Value: resp.Result})
	}
	for _, mut := range resp.Memory {
		scope := memory.MemoryScope(mut.Scope)
		if scope == "" {
			scope = memory.ScopeExecution
		}
		if scope != memory.ScopeExecution {
			return fail(WorkerProtocolError, fmt.Errorf("worker memory mutations must be execution-scoped"))
		}
	}
	if resp.GraphMutation != nil && (resp.GraphMutation.Action != "delegate" || resp.GraphMutation.TargetAgent == "") {
		return fail(WorkerProtocolError, fmt.Errorf("invalid graph mutation"))
	}
	// Commit memory and artifact as one acknowledged durable result.
	entries := make([]memory.MemoryEntry, 0, len(resp.Memory))
	for _, mut := range resp.Memory {
		entries = append(entries, memory.MemoryEntry{Key: mut.Key, Value: mut.Value, Scope: memory.ScopeExecution, ScopeID: req.ExecutionID, Owner: req.AgentID, ExpectedVersion: mut.ExpectedVersion})
	}
	if a := resp.Artifact; a != nil {
		a.Producer = string(w.ID)
		a.Task = string(req.ID)
		a.Execution = req.ExecutionID
		a.Workflow = req.Workflow
		a.CreatedAt = time.Now()
		for _, input := range req.Inputs {
			a.Parents = append(a.Parents, memory.ArtifactID(input.ArtifactID))
		}
	}
	if len(entries) > 0 || resp.Artifact != nil {
		result := make(chan error, 1)
		w.Bus.Publish("TaskResultCommitRequested", "worker", memory.ResultCommitRequest{Entries: entries, Artifact: resp.Artifact, Result: result})
		select {
		case err := <-result:
			if err != nil {
				return fail(WorkerProtocolError, fmt.Errorf("result commit rejected: %w", err))
			}
		case <-ctx.Done():
			return fail(WorkerTimeout, ctx.Err())
		}
	}
	if resp.GraphMutation != nil {
		w.Bus.Publish("GraphMutationRequested", "worker", map[string]any{"task_id": req.ID, "worker_id": w.ID, "execution": req.ExecutionID, "mutation": resp.GraphMutation})
	}
	w.Bus.Publish("WorkerCompleted", "worker", map[string]any{"task_id": string(req.ID), "worker_id": string(w.ID)})
	return &resp, nil
}

// Always drain child pipes, retaining only a bounded prefix.
type limitedOutput struct {
	bytes.Buffer
	limit    int
	exceeded bool
	onChunk  func([]byte)
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.Len()
	if n > remaining {
		b.exceeded = true
		p = p[:remaining]
	}
	if b.onChunk != nil && len(p) > 0 {
		b.onChunk(p)
	}
	b.Buffer.Write(p)
	return n, nil
}

func workerEnvironment(req Task, explicit []string) []string {
	allowed := map[string]bool{"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "TEMP": true, "TMP": true, "HOME": true, "USERPROFILE": true, "LANG": true, "LC_ALL": true, "PYTHONIOENCODING": true, "OLLAMA_HOST": true, "COMFYUI_HOST": true, "COMFYUI_CHECKPOINT": true, "RETICLE_WORKER_IMAGE": true, "RETICLE_COMMAND_TIMEOUT": true, "RETICLE_MEMORY_LIMIT": true, "RETICLE_CPU_LIMIT": true}
	if key, ok := req.Parameters["api_key"].(string); ok && key != "" {
		// A model-auth parameter cannot request an arbitrary parent secret.
		switch key {
		case "OPENROUTER_API_KEY", "OPENROUTER_API_KEY_2", "OPENROUTER_API_KEY_3", "GROQ_API_KEY", "GROQ_API_KEY_2", "GROQ_API_KEY_3", "GEMINI_API_KEY":
			allowed[key] = true
		}
	}
	if req.AgentID == "ml-agent" {
		for _, name := range []string{"HF_TOKEN", "WANDB_API_KEY", "RETICLE_GPU_DEVICES"} {
			allowed[name] = true
		}
	}
	env := []string{"PYTHONIOENCODING=utf-8", "PYTHONUNBUFFERED=1"}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if allowed[strings.ToUpper(key)] {
			env = append(env, item)
		}
	}
	return append(env, explicit...)
}
