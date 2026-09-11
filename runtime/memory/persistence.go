package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const snapshotFormatVersion = 1

type persistedState struct {
	FormatVersion int                        `json:"format_version"`
	Runtime       []MemoryEntry              `json:"runtime"`
	Artifacts     map[ArtifactID][]*Artifact `json:"artifacts"`
}

// SnapshotFile provides atomic, permission-restricted restart snapshots.
// Save is synchronous so a successful call means the rename has completed.
type SnapshotFile struct {
	path string
	mu   sync.Mutex
}

func NewSnapshotFile(path string) *SnapshotFile { return &SnapshotFile{path: path} }

func (f *SnapshotFile) Load(runtime *RuntimeState, artifacts *ArtifactStore) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := os.ReadFile(f.path)
	if os.IsNotExist(err) {
		data, err = os.ReadFile(f.path + ".previous")
	}
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read memory snapshot: %w", err)
	}
	var snapshot persistedState
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode memory snapshot: %w", err)
	}
	if snapshot.FormatVersion != snapshotFormatVersion {
		return fmt.Errorf("unsupported memory snapshot format %d", snapshot.FormatVersion)
	}
	if err := runtime.Restore(snapshot.Runtime); err != nil {
		return fmt.Errorf("restore runtime memory: %w", err)
	}
	if err := artifacts.Restore(snapshot.Artifacts); err != nil {
		return fmt.Errorf("restore artifacts: %w", err)
	}
	return nil
}

func (f *SnapshotFile) Save(runtime *RuntimeState, artifacts *ArtifactStore) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot := persistedState{FormatVersion: snapshotFormatVersion, Runtime: runtime.Snapshot(), Artifacts: artifacts.Snapshot()}
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
