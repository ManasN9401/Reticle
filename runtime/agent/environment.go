package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/reticle/runtime/events"
	"github.com/reticle/runtime/logger"
)

type EnvironmentManager struct {
	Logger   *logger.Logger
	Bus      *events.Bus
	BaseDir  string
	baseMu   sync.Mutex
	reported map[string]bool
}

type provisioningContext struct {
	executionID string
	taskID      string
	attemptID   string
}

func newProvisioningContext(tasks []Task) provisioningContext {
	if len(tasks) == 0 {
		return provisioningContext{}
	}
	return provisioningContext{
		executionID: tasks[0].ExecutionID,
		taskID:      string(tasks[0].ID),
		attemptID:   tasks[0].AttemptID,
	}
}

func (p provisioningContext) operationID(scope string) string {
	identity := p.attemptID
	if identity == "" {
		identity = p.taskID
	}
	if identity == "" {
		identity = "runtime"
	}
	return identity + ":environment:" + scope
}

// NewEnvironmentManager initializes an environment manager that stores venvs in <root>/.reticle/envs
func NewEnvironmentManager(l *logger.Logger, rootDir string, buses ...*events.Bus) *EnvironmentManager {
	manager := &EnvironmentManager{
		Logger:   l,
		BaseDir:  filepath.Join(rootDir, ".reticle", "envs"),
		reported: make(map[string]bool),
	}
	if len(buses) > 0 {
		manager.Bus = buses[0]
	}
	return manager
}

func (em *EnvironmentManager) firstReport(key string) bool {
	if em.reported == nil {
		em.reported = make(map[string]bool)
	}
	if em.reported[key] {
		return false
	}
	em.reported[key] = true
	return true
}

func (em *EnvironmentManager) publishProvisioning(event string, agentID WorkerID, dependencies []string, hasUV, cached bool, duration time.Duration, err error, context provisioningContext) {
	if em.Bus == nil {
		return
	}
	payload := map[string]any{
		"scope":        "base",
		"operation_id": context.operationID("base"),
		"dependencies": append([]string(nil), dependencies...),
		"using_uv":     hasUV,
		"cached":       cached,
	}
	if agentID != "" {
		payload["scope"] = "agent"
		payload["agent_id"] = agentID
		payload["operation_id"] = context.operationID("agent:" + string(agentID))
	}
	if context.executionID != "" {
		payload["execution"] = context.executionID
	}
	if context.taskID != "" {
		payload["task_id"] = context.taskID
	}
	if context.attemptID != "" {
		payload["attempt_id"] = context.attemptID
	}
	if duration > 0 {
		payload["duration_ms"] = duration.Milliseconds()
	}
	if err != nil {
		payload["error"] = logger.Redact(err.Error())
	}
	em.Bus.Publish(events.EventType(event), events.Component("environment"), payload)
}

// Provision prepares the virtual environment for a worker and installs the aggregated skill dependencies.
// It returns the path to the virtual python executable, and the aggregated env vars.
func (em *EnvironmentManager) Provision(agentID WorkerID, skills []SkillDefinition) (string, []string, error) {
	return em.ProvisionContext(context.Background(), agentID, skills)
}
func (em *EnvironmentManager) ProvisionContext(parent context.Context, agentID WorkerID, skills []SkillDefinition, tasks ...Task) (string, []string, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
	defer cancel()
	provisioning := newProvisioningContext(tasks)
	if err := os.MkdirAll(em.BaseDir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create env base dir: %w", err)
	}

	hasUv := environmentCommand(ctx, "uv", "--version").Run() == nil

	// Ensure base environment exists
	for !em.baseMu.TryLock() {
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	defer em.baseMu.Unlock()
	lockPath := filepath.Join(em.BaseDir, ".provision-lock")
	var lock *os.File
	var lockErr error
	for attempt := 0; attempt < 120; attempt++ {
		lock, lockErr = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if lockErr == nil {
			break
		}
		if !os.IsExist(lockErr) {
			return "", nil, lockErr
		}
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if lockErr != nil {
		return "", nil, fmt.Errorf("environment provisioning lock unavailable: %w", lockErr)
	}
	lock.Close()
	defer os.Remove(lockPath)
	baseEnvPath := filepath.Join(em.BaseDir, "forge-base")
	pythonExeWin := filepath.Join(baseEnvPath, "Scripts", "python.exe")
	pythonExeUnix := filepath.Join(baseEnvPath, "bin", "python")

	baseCreated := false
	if _, err := os.Stat(pythonExeWin); os.IsNotExist(err) {
		if _, err := os.Stat(pythonExeUnix); os.IsNotExist(err) {
			em.Logger.Info("Creating base environment", "path", baseEnvPath, "using_uv", hasUv)

			var cmd *exec.Cmd
			if hasUv {
				cmd = environmentCommand(ctx, "uv", "venv", baseEnvPath)
			} else {
				cmd = environmentCommand(ctx, "python", "-m", "venv", baseEnvPath)
			}

			if out, err := cmd.CombinedOutput(); err != nil {

				return "", nil, fmt.Errorf("failed to create base venv to %s: %s - %w", baseEnvPath, string(out), err)
			}
			baseCreated = true
		}
	}

	basePythonExe := pythonExeWin
	if _, err := os.Stat(basePythonExe); os.IsNotExist(err) {
		basePythonExe = pythonExeUnix
	}
	pythonVersion, err := environmentCommand(ctx, basePythonExe, "--version").CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("failed to identify worker interpreter: %s - %w", string(pythonVersion), err)
	}

	// Install base dependencies if needed
	baseDeps := []string{"litellm==1.99.0", "requests==2.34.2", "tenacity==9.1.4"}
	baseDepsString := strings.Join(baseDeps, "\n")
	baseDepsFile := filepath.Join(baseEnvPath, ".deps")
	constraintsFile := filepath.Join(baseEnvPath, "constraints.txt")
	if err := os.WriteFile(constraintsFile, []byte(baseDepsString), 0600); err != nil {
		return "", nil, err
	}

	needsBaseInstall := baseCreated
	if !needsBaseInstall {
		existingDeps, err := os.ReadFile(baseDepsFile)
		if err != nil || string(existingDeps) != baseDepsString {
			needsBaseInstall = true
		}
	}

	if needsBaseInstall {
		startedAt := time.Now()
		em.Logger.Info("Installing base dependencies", "deps", baseDeps, "using_uv", hasUv)
		em.publishProvisioning("EnvironmentProvisioningStarted", "", baseDeps, hasUv, false, 0, nil, provisioning)
		var cmd *exec.Cmd
		if hasUv {
			args := append([]string{"pip", "install", "--python", basePythonExe}, baseDeps...)
			cmd = environmentCommand(ctx, "uv", args...)
		} else {
			args := append([]string{"-m", "pip", "install"}, baseDeps...)
			cmd = environmentCommand(ctx, basePythonExe, args...)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			em.Logger.Error("Base dependency installation failed", "deps", baseDeps, "using_uv", hasUv, "error", err)
			em.publishProvisioning("EnvironmentProvisioningFailed", "", baseDeps, hasUv, false, time.Since(startedAt), err, provisioning)
			return "", nil, fmt.Errorf("base install failed: %s - %w", string(out), err)
		}
		if err := os.WriteFile(baseDepsFile, []byte(baseDepsString), 0600); err != nil {
			return "", nil, err
		}
		em.Logger.Info("Base dependencies ready", "deps", baseDeps, "using_uv", hasUv, "cached", false, "duration_ms", time.Since(startedAt).Milliseconds())
		em.publishProvisioning("EnvironmentProvisioningCompleted", "", baseDeps, hasUv, false, time.Since(startedAt), nil, provisioning)
		em.firstReport("base")
	} else if em.firstReport("base") {
		em.Logger.Info("Base dependencies ready", "deps", baseDeps, "using_uv", hasUv, "cached", true)
		em.publishProvisioning("EnvironmentProvisioningCompleted", "", baseDeps, hasUv, true, 0, nil, provisioning)
	}

	// Aggregate agent-specific dependencies and env vars
	var dependencies []string
	var envVars []string

	for _, skill := range skills {
		dependencies = append(dependencies, skill.Dependencies...)
		for _, v := range skill.EnvVars {
			envVars = append(envVars, os.ExpandEnv(v))
		}
	}

	if len(dependencies) > 0 {
		// Use a mutex to prevent race conditions on agent-specific libs if the same agent runs concurrently
		identity := strings.Join([]string{basePythonExe, strings.TrimSpace(string(pythonVersion)), runtime.GOOS, runtime.GOARCH, baseDepsString, strings.Join(dependencies, "\n")}, "\n")
		envHash := sha256.Sum256([]byte(identity))
		agentLibsPath := filepath.Join(em.BaseDir, fmt.Sprintf("%s-%x-libs", strings.TrimPrefix(string(agentID), strings.Split(string(agentID), "__")[0]+"__"), envHash[:8]))
		if err := os.MkdirAll(agentLibsPath, 0755); err != nil {
			return "", nil, fmt.Errorf("failed to create agent libs dir: %w", err)
		}

		depsString := strings.Join(dependencies, "\n")
		depsFile := filepath.Join(agentLibsPath, ".deps")

		needsInstall := false
		existingDeps, err := os.ReadFile(depsFile)
		if err != nil || string(existingDeps) != depsString {
			needsInstall = true
		}

		if needsInstall {
			startedAt := time.Now()
			em.Logger.Info("Installing agent dependencies", "agent_id", agentID, "deps", dependencies, "using_uv", hasUv)
			em.publishProvisioning("EnvironmentProvisioningStarted", agentID, dependencies, hasUv, false, 0, nil, provisioning)

			var cmd *exec.Cmd
			if hasUv {
				args := append([]string{"pip", "install", "--constraint", constraintsFile, "--target", agentLibsPath, "--python", basePythonExe}, dependencies...)
				cmd = environmentCommand(ctx, "uv", args...)
			} else {
				args := append([]string{"-m", "pip", "install", "--constraint", constraintsFile, "--target", agentLibsPath}, dependencies...)
				cmd = environmentCommand(ctx, basePythonExe, args...)
			}

			if out, err := cmd.CombinedOutput(); err != nil {
				em.Logger.Error("Agent dependency installation failed", "agent_id", agentID, "deps", dependencies, "using_uv", hasUv, "error", err)
				em.publishProvisioning("EnvironmentProvisioningFailed", agentID, dependencies, hasUv, false, time.Since(startedAt), err, provisioning)
				return "", nil, fmt.Errorf("agent install failed: %s - %w", string(out), err)
			}
			if err := os.WriteFile(depsFile, []byte(depsString), 0600); err != nil {
				return "", nil, err
			}
			em.Logger.Info("Agent dependencies ready", "agent_id", agentID, "deps", dependencies, "using_uv", hasUv, "cached", false, "duration_ms", time.Since(startedAt).Milliseconds())
			em.publishProvisioning("EnvironmentProvisioningCompleted", agentID, dependencies, hasUv, false, time.Since(startedAt), nil, provisioning)
			em.firstReport("agent:" + string(agentID))
		} else if em.firstReport("agent:" + string(agentID)) {
			em.Logger.Info("Agent dependencies ready", "agent_id", agentID, "deps", dependencies, "using_uv", hasUv, "cached", true)
			em.publishProvisioning("EnvironmentProvisioningCompleted", agentID, dependencies, hasUv, true, 0, nil, provisioning)
		}

		// Add PYTHONPATH to envVars
		envVars = append(envVars, fmt.Sprintf("PYTHONPATH=%s", agentLibsPath))
	}

	return basePythonExe, envVars, nil
}

func environmentCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	configureProcess(cmd)
	cmd.WaitDelay = 2 * time.Second
	return cmd
}
