package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const executionSnapshotVersion = 1

type executionSnapshot struct {
	FormatVersion int                           `json:"format_version"`
	Executions    map[string]*WorkflowExecution `json:"executions"`
	Cancelled     map[string]bool               `json:"cancelled,omitempty"`
	Paused        map[string]bool               `json:"paused,omitempty"`
}

// ExecutionStore is the local durable boundary for run, node, attempt, graph,
// artifact-reference and lifecycle state. It intentionally supports one writer.
// A multi-process runtime requires a transactional database-backed implementation.
type ExecutionStore struct {
	path string
	mu   sync.Mutex
}

func NewExecutionStore(path string) *ExecutionStore { return &ExecutionStore{path: path} }

func (s *ExecutionStore) Load() (executionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := executionSnapshot{
		FormatVersion: executionSnapshotVersion,
		Executions:    make(map[string]*WorkflowExecution),
		Cancelled:     make(map[string]bool),
		Paused:        make(map[string]bool),
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		data, err = os.ReadFile(s.path + ".previous")
	}
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, fmt.Errorf("read execution snapshot: %w", err)
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, fmt.Errorf("decode execution snapshot: %w", err)
	}
	if snapshot.FormatVersion != executionSnapshotVersion {
		return snapshot, fmt.Errorf("unsupported execution snapshot format %d", snapshot.FormatVersion)
	}
	if snapshot.Executions == nil {
		snapshot.Executions = make(map[string]*WorkflowExecution)
	}
	if snapshot.Cancelled == nil {
		snapshot.Cancelled = make(map[string]bool)
	}
	if snapshot.Paused == nil {
		snapshot.Paused = make(map[string]bool)
	}
	return snapshot, nil
}

func (s *ExecutionStore) Save(executions map[string]*WorkflowExecution, cancelled, paused map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(executionSnapshot{
		FormatVersion: executionSnapshotVersion,
		Executions:    executions,
		Cancelled:     cancelled,
		Paused:        paused,
	})
	if err != nil {
		return fmt.Errorf("encode execution snapshot: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create execution snapshot directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".executions-*.tmp")
	if err != nil {
		return fmt.Errorf("create execution snapshot: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write execution snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync execution snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close execution snapshot: %w", err)
	}
	// Windows can transiently deny a rename over state.json while an external
	// process (antivirus real-time scan, search indexer, backup agent) has it
	// briefly open without FILE_SHARE_DELETE. A single failure here used to
	// kill an otherwise-healthy execution outright (RuntimePersistenceFailed,
	// "restart required") for what is normally a lock held for milliseconds —
	// retry the replace a few times with a short backoff before giving up.
	var replaceErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
		}
		replaceErr = func() error {
			if err := os.Rename(tmpName, s.path); err != nil {
				backup := s.path + ".previous"
				_ = os.Remove(backup)
				if moveErr := os.Rename(s.path, backup); moveErr != nil && !os.IsNotExist(moveErr) {
					return fmt.Errorf("prepare execution snapshot replacement: %w", err)
				}
				if moveErr := os.Rename(tmpName, s.path); moveErr != nil {
					_ = os.Rename(backup, s.path)
					return fmt.Errorf("replace execution snapshot: %w", moveErr)
				}
				_ = os.Remove(backup)
			}
			return nil
		}()
		if replaceErr == nil {
			return nil
		}
	}
	return replaceErr
}
