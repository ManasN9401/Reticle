package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hyperparallel/runtime/logger"
)

type EnvironmentManager struct {
	Logger  *logger.Logger
	BaseDir string
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
	envPath := filepath.Join(em.BaseDir, string(agentID))

	if err := os.MkdirAll(em.BaseDir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create env base dir: %w", err)
	}

	// 2. Check if uv is available
	hasUv := exec.Command("uv", "--version").Run() == nil

	// 3. Create virtual environment if it doesn't exist
	pythonExeWin := filepath.Join(envPath, "Scripts", "python.exe")
	pythonExeUnix := filepath.Join(envPath, "bin", "python")
	
	venvCreated := false
	if _, err := os.Stat(pythonExeWin); os.IsNotExist(err) {
		if _, err := os.Stat(pythonExeUnix); os.IsNotExist(err) {
			em.Logger.Info("Creating virtual environment", "agent_id", agentID, "path", envPath, "using_uv", hasUv)
			
			var cmd *exec.Cmd
			if hasUv {
				cmd = exec.Command("uv", "venv", envPath)
			} else {
				cmd = exec.Command("python", "-m", "venv", envPath)
			}
			
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", nil, fmt.Errorf("failed to create venv to %s: %s - %w", envPath, string(out), err)
			}
			venvCreated = true
		}
	}

	// Determine platform-specific python executable
	pythonExe := pythonExeWin
	if _, err := os.Stat(pythonExe); os.IsNotExist(err) {
		pythonExe = pythonExeUnix
	}

	// 4. Aggregate dependencies and env vars from all inherited skills
	var dependencies []string
	var envVars []string

	// Always require litellm and requests for Forge workers
	dependencies = append(dependencies, "litellm", "requests")

	for _, skill := range skills {
		dependencies = append(dependencies, skill.Dependencies...)
		for _, v := range skill.EnvVars {
			envVars = append(envVars, os.ExpandEnv(v))
		}
	}

	// 4. Install required dependencies if newly created OR dependencies changed
	if len(dependencies) > 0 {
		depsString := strings.Join(dependencies, "\n")
		depsFile := filepath.Join(envPath, ".deps")
		needsInstall := venvCreated
		
		if !needsInstall {
			existingDeps, err := os.ReadFile(depsFile)
			if err != nil || string(existingDeps) != depsString {
				needsInstall = true
			}
		}

		if needsInstall {
			em.Logger.Info("Installing skill dependencies", "agent_id", agentID, "deps", dependencies, "using_uv", hasUv)
			
			var cmd *exec.Cmd
			if hasUv {
				args := append([]string{"pip", "install", "--python", pythonExe}, dependencies...)
				cmd = exec.Command("uv", args...)
			} else {
				args := append([]string{"-m", "pip", "install"}, dependencies...)
				cmd = exec.Command(pythonExe, args...)
			}
			
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", nil, fmt.Errorf("install failed: %s - %w", string(out), err)
			}
			os.WriteFile(depsFile, []byte(depsString), 0644)
		}
	}

	return pythonExe, envVars, nil
}


