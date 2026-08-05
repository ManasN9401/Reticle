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
	baseEnvPath := filepath.Join(em.BaseDir, "_base")

	if err := os.MkdirAll(em.BaseDir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create env base dir: %w", err)
	}

	// 1. Ensure Base Venv exists
	basePythonExeWin := filepath.Join(baseEnvPath, "Scripts", "python.exe")
	basePythonExeUnix := filepath.Join(baseEnvPath, "bin", "python")

	if _, err := os.Stat(basePythonExeWin); os.IsNotExist(err) {
		if _, err := os.Stat(basePythonExeUnix); os.IsNotExist(err) {
			em.Logger.Info("Creating base virtual environment", "path", baseEnvPath)
			cmd := exec.Command("python", "-m", "venv", baseEnvPath)
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", nil, fmt.Errorf("failed to create base venv: %s - %w", string(out), err)
			}
		}
	}

	// 2. Inherit Base Venv via system-site-packages if Agent Env doesn't exist
	pythonExeWin := filepath.Join(envPath, "Scripts", "python.exe")
	pythonExeUnix := filepath.Join(envPath, "bin", "python")

	if _, err := os.Stat(pythonExeWin); os.IsNotExist(err) {
		if _, err := os.Stat(pythonExeUnix); os.IsNotExist(err) {
			em.Logger.Info("Inheriting base virtual environment", "agent_id", agentID, "path", envPath)
			basePythonExe := basePythonExeWin
			if _, err := os.Stat(basePythonExe); os.IsNotExist(err) {
				basePythonExe = basePythonExeUnix
			}
			cmd := exec.Command(basePythonExe, "-m", "venv", "--system-site-packages", envPath)
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", nil, fmt.Errorf("failed to inherit base venv to %s: %s - %w", envPath, string(out), err)
			}
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

	for _, skill := range skills {
		dependencies = append(dependencies, skill.Dependencies...)
		for _, v := range skill.EnvVars {
			envVars = append(envVars, os.ExpandEnv(v))
		}
	}

	// 4. Install required dependencies
	if len(dependencies) > 0 {
		em.Logger.Info("Installing skill dependencies", "agent_id", agentID, "deps", dependencies)
		args := append([]string{"-m", "pip", "install"}, dependencies...)
		cmd := exec.Command(pythonExe, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", nil, fmt.Errorf("pip install failed: %s - %w", string(out), err)
		}
	}

	return pythonExe, envVars, nil
}


