package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const snapshotFormatVersion = 2

type persistedState struct {
	FormatVersion     int                        `json:"format_version"`
	Runtime           []MemoryEntry              `json:"runtime"`
	Artifacts         map[ArtifactID][]*Artifact `json:"artifacts"`
	CommittedAttempts []string                   `json:"committed_attempts,omitempty"`
}

// SnapshotFile provides atomic, permission-restricted restart snapshots.
// Save is synchronous so a successful call means the rename has completed.
type SnapshotFile struct {
	path string
	mu   sync.Mutex
}

func NewSnapshotFile(path string) *SnapshotFile { return &SnapshotFile{path: path} }

func (f *SnapshotFile) Load(runtime *RuntimeState, artifacts *ArtifactStore) error {
	_, err := f.LoadWithCommits(runtime, artifacts)
	return err
}

func (f *SnapshotFile) LoadWithCommits(runtime *RuntimeState, artifacts *ArtifactStore) (map[string]struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := os.ReadFile(f.path)
	if os.IsNotExist(err) {
		data, err = os.ReadFile(f.path + ".previous")
	}
	if os.IsNotExist(err) {
		return make(map[string]struct{}), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read memory snapshot: %w", err)
	}
	var snapshot persistedState
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("decode memory snapshot: %w", err)
	}
	if snapshot.FormatVersion != 1 && snapshot.FormatVersion != snapshotFormatVersion {
		return nil, fmt.Errorf("unsupported memory snapshot format %d", snapshot.FormatVersion)
	}
	if err := runtime.Restore(snapshot.Runtime); err != nil {
		return nil, fmt.Errorf("restore runtime memory: %w", err)
	}
	if err := artifacts.Restore(snapshot.Artifacts); err != nil {
		return nil, fmt.Errorf("restore artifacts: %w", err)
	}
	committed := make(map[string]struct{}, len(snapshot.CommittedAttempts))
	for _, id := range snapshot.CommittedAttempts {
		if id != "" {
			committed[id] = struct{}{}
		}
	}
	return committed, nil
}

func (f *SnapshotFile) Save(runtime *RuntimeState, artifacts *ArtifactStore) error {
	return f.SaveWithCommits(runtime, artifacts, nil)
}

func (f *SnapshotFile) SaveWithCommits(runtime *RuntimeState, artifacts *ArtifactStore, committed map[string]struct{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	committedIDs := make([]string, 0, len(committed))
	for id := range committed {
		committedIDs = append(committedIDs, id)
	}
	snapshot := persistedState{FormatVersion: snapshotFormatVersion, Runtime: runtime.Snapshot(), Artifacts: artifacts.Snapshot(), CommittedAttempts: committedIDs}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode memory snapshot: %w", err)
	}
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create memory snapshot directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create memory snapshot: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write memory snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync memory snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close memory snapshot: %w", err)
	}
	if err := os.Rename(tmpName, f.path); err != nil {
		// Windows does not replace an existing destination atomically. Preserve a
		// backup until the replacement succeeds so restart recovery still has a copy.
		backup := f.path + ".previous"
		_ = os.Remove(backup)
		if moveErr := os.Rename(f.path, backup); moveErr != nil && !os.IsNotExist(moveErr) {
			return fmt.Errorf("prepare memory snapshot replacement: %w", err)
		}
		if moveErr := os.Rename(tmpName, f.path); moveErr != nil {
			_ = os.Rename(backup, f.path)
			return fmt.Errorf("replace memory snapshot: %w", moveErr)
		}
		_ = os.Remove(backup)
	}
	return nil
}
