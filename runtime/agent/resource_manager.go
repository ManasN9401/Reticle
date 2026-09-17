package agent

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type MLHostEnvironment string

const (
	MLHostWindows MLHostEnvironment = "windows"
	MLHostWSL2    MLHostEnvironment = "wsl2"
	MLHostLinux   MLHostEnvironment = "linux"
	MLHostOther   MLHostEnvironment = "other"
)

func DetectMLHostEnvironment() MLHostEnvironment {
	release := ""
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
			release = string(data)
		}
	}
	return classifyMLHost(runtime.GOOS, release, os.Getenv("WSL_INTEROP"))
}

func classifyMLHost(goos, release, wslInterop string) MLHostEnvironment {
	switch strings.ToLower(goos) {
	case "windows":
		return MLHostWindows
	case "linux":
		if wslInterop != "" || strings.Contains(strings.ToLower(release), "microsoft") {
			return MLHostWSL2
		}
		return MLHostLinux
	default:
		return MLHostOther
	}
}

func AssessMLProfile(profile string, host MLHostEnvironment, devices []string) ([]string, error) {
	profile = strings.ToLower(strings.TrimSpace(profile))
	if profile == "" {
		profile = "cpu"
	}
	if profile != "cpu" && profile != "amd-rocm" && profile != "nvidia-cuda" && profile != "user" {
		return nil, fmt.Errorf("RETICLE_ML_PROFILE must be cpu, amd-rocm, nvidia-cuda, or user")
	}
	if profile == "cpu" && len(devices) > 0 {
		return nil, fmt.Errorf("CPU profile cannot request accelerator devices")
	}
	if (profile == "amd-rocm" || profile == "nvidia-cuda") && len(devices) == 0 {
		return nil, fmt.Errorf("%s profile requires explicit RETICLE_GPU_DEVICES", profile)
	}
	warnings := make([]string, 0, 1)
	if profile == "amd-rocm" && host == MLHostWindows {
		warnings = append(warnings, "Native Windows AMD support is limited to PyTorch and AMD's current listed GPUs; training is not supported by the current Windows stack. Verify the RX 7800 XT against the current AMD matrix before relying on acceleration.")
	} else if profile == "amd-rocm" && host == MLHostWSL2 {
		warnings = append(warnings, "WSL2 detected: verify the exact Windows driver, WSL distribution, ROCm image and RX 7800 XT support before running an experiment.")
	}
	return warnings, nil
}

// DeviceLeaseManager prevents two tasks in one runtime from being admitted to
// the same explicitly named accelerator at the same time.
type DeviceLeaseManager struct {
	mu     sync.Mutex
	owners map[string]string
}

func NewDeviceLeaseManager() *DeviceLeaseManager {
	return &DeviceLeaseManager{owners: make(map[string]string)}
}

func ParseDeviceList(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	devices := make([]string, 0)
	for _, raw := range strings.Split(value, ",") {
		device := strings.TrimSpace(raw)
		if device == "" {
			return nil, fmt.Errorf("empty accelerator device")
		}
		for _, r := range device {
			if r < '0' || r > '9' {
				return nil, fmt.Errorf("invalid accelerator device %q", device)
			}
		}
		if _, exists := seen[device]; exists {
			continue
		}
		seen[device] = struct{}{}
		devices = append(devices, device)
	}
	sort.Strings(devices)
	return devices, nil
}

func (m *DeviceLeaseManager) Acquire(ctx context.Context, owner string, devices []string) (func(), error) {
	if len(devices) == 0 {
		return func() {}, nil
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		available := true
		for _, device := range devices {
			if current := m.owners[device]; current != "" && current != owner {
				available = false
				break
			}
		}
		if available {
			for _, device := range devices {
				m.owners[device] = owner
			}
			m.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					m.mu.Lock()
					defer m.mu.Unlock()
					for _, device := range devices {
						if m.owners[device] == owner {
							delete(m.owners, device)
						}
					}
				})
			}, nil
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
