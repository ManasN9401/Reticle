package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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

	venvCreated := false
	pythonExeWin := filepath.Join(envPath, "Scripts", "python.exe")
	pythonExeUnix := filepath.Join(envPath, "bin", "python")

	if _, err := os.Stat(pythonExeWin); os.IsNotExist(err) {
		if _, err := os.Stat(pythonExeUnix); os.IsNotExist(err) {
			em.Logger.Info("Creating virtual environment", "agent_id", agentID, "path", envPath)
			cmd := exec.Command("python", "-m", "venv", envPath)
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

	// 3. Aggregate dependencies and env vars from all inherited skills
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

	// 4. Install required dependencies only if newly created
	if venvCreated && len(dependencies) > 0 {
		em.Logger.Info("Installing skill dependencies", "agent_id", agentID, "deps", dependencies)
		args := append([]string{"-m", "pip", "install"}, dependencies...)
		cmd := exec.Command(pythonExe, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", nil, fmt.Errorf("pip install failed: %s - %w", string(out), err)
		}
	}

	return pythonExe, envVars, nil
}


