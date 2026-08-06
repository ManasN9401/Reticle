package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hyperparallel/runtime/logger"
)

type EnvironmentManager struct {
	Logger  *logger.Logger
	BaseDir string
	baseMu  sync.Mutex
}

// NewEnvironmentManager initializes an environment manager that stores venvs in <root>/.hyperparallel/envs
func NewEnvironmentManager(l *logger.Logger, rootDir string) *EnvironmentManager {
	return &EnvironmentManager{
		Logger:  l,
		BaseDir: filepath.Join(rootDir, ".hyperparallel", "envs"),
	}
}

// Provision prepares the virtual environment for a worker and installs the aggregated skill dependencies.
// It returns the path to the virtual python executable, and the aggregated env vars.
func (em *EnvironmentManager) Provision(agentID WorkerID, skills []SkillDefinition) (string, []string, error) {
	if err := os.MkdirAll(em.BaseDir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create env base dir: %w", err)
	}

	hasUv := exec.Command("uv", "--version").Run() == nil

	// Ensure base environment exists
	em.baseMu.Lock()
	baseEnvPath := filepath.Join(em.BaseDir, "forge-base")
	pythonExeWin := filepath.Join(baseEnvPath, "Scripts", "python.exe")
	pythonExeUnix := filepath.Join(baseEnvPath, "bin", "python")
	
	baseCreated := false
	if _, err := os.Stat(pythonExeWin); os.IsNotExist(err) {
		if _, err := os.Stat(pythonExeUnix); os.IsNotExist(err) {
			em.Logger.Info("Creating base environment", "path", baseEnvPath, "using_uv", hasUv)
			
			var cmd *exec.Cmd
			if hasUv {
				cmd = exec.Command("uv", "venv", baseEnvPath)
			} else {
				cmd = exec.Command("python", "-m", "venv", baseEnvPath)
			}
			
			if out, err := cmd.CombinedOutput(); err != nil {
				em.baseMu.Unlock()
				return "", nil, fmt.Errorf("failed to create base venv to %s: %s - %w", baseEnvPath, string(out), err)
			}
			baseCreated = true
		}
	}

	basePythonExe := pythonExeWin
	if _, err := os.Stat(basePythonExe); os.IsNotExist(err) {
		basePythonExe = pythonExeUnix
	}

	// Install base dependencies if needed
	baseDeps := []string{"litellm", "requests", "tenacity"}
	baseDepsString := strings.Join(baseDeps, "\n")
	baseDepsFile := filepath.Join(baseEnvPath, ".deps")
	
	needsBaseInstall := baseCreated
	if !needsBaseInstall {
		existingDeps, err := os.ReadFile(baseDepsFile)
		if err != nil || string(existingDeps) != baseDepsString {
			needsBaseInstall = true
		}
	}

	if needsBaseInstall {
		em.Logger.Info("Installing base dependencies", "deps", baseDeps, "using_uv", hasUv)
		var cmd *exec.Cmd
		if hasUv {
			args := append([]string{"pip", "install", "--python", basePythonExe}, baseDeps...)
			cmd = exec.Command("uv", args...)
		} else {
			args := append([]string{"-m", "pip", "install"}, baseDeps...)
			cmd = exec.Command(basePythonExe, args...)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			em.baseMu.Unlock()
			return "", nil, fmt.Errorf("base install failed: %s - %w", string(out), err)
		}
		os.WriteFile(baseDepsFile, []byte(baseDepsString), 0644)
	}
	em.baseMu.Unlock()

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
		agentLibsPath := filepath.Join(em.BaseDir, fmt.Sprintf("%s-libs", agentID))
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
			em.Logger.Info("Installing agent dependencies", "agent_id", agentID, "deps", dependencies, "using_uv", hasUv)
			
			var cmd *exec.Cmd
			if hasUv {
				args := append([]string{"pip", "install", "--target", agentLibsPath, "--python", basePythonExe}, dependencies...)
				cmd = exec.Command("uv", args...)
			} else {
				args := append([]string{"-m", "pip", "install", "--target", agentLibsPath}, dependencies...)
				cmd = exec.Command(basePythonExe, args...)
			}
			
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", nil, fmt.Errorf("agent install failed: %s - %w", string(out), err)
			}
			os.WriteFile(depsFile, []byte(depsString), 0644)
		}
		
		// Add PYTHONPATH to envVars
		envVars = append(envVars, fmt.Sprintf("PYTHONPATH=%s", agentLibsPath))
	}

	return basePythonExe, envVars, nil
}


