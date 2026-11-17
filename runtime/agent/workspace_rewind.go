package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type rewindFile struct {
	Path       string  `json:"path"`
	Before     []byte  `json:"before"`
	After      []byte  `json:"after"`
	BeforeHash *string `json:"before_sha256"`
	AfterHash  *string `json:"after_sha256"`
	Mode       uint32  `json:"mode"`
}
type rewindJournal struct {
	CreatedAt time.Time    `json:"created_at"`
	Operation string       `json:"operation"`
	Version   int          `json:"version"`
	State     string       `json:"state"`
	TaskID    string       `json:"task_id"`
	AttemptID string       `json:"attempt_id"`
	Files     []rewindFile `json:"files"`
}
type checkpointTreeEntry struct {
	OID  string
	Mode string
}

func (h *WorkspaceHistory) tree(sha string) (map[string]checkpointTreeEntry, error) {
	raw, err := h.git(nil, "ls-tree", "-r", "-z", sha)
	if err != nil {
		return nil, err
	}
	result := map[string]checkpointTreeEntry{}
	for _, line := range strings.Split(string(raw), "\x00") {
		if line == "" {
			continue
		}
		header, path, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("invalid checkpoint tree")
		}
		parts := strings.Fields(header)
		if len(parts) != 3 || parts[1] != "blob" || (parts[0] != "100644" && parts[0] != "100755") {
			return nil, fmt.Errorf("unsupported checkpoint tree entry")
		}
		if ignoredHistoryPath(path) {
			return nil, fmt.Errorf("checkpoint contains excluded path")
		}
		if _, err = safeHistoryPath(h.source, path); err != nil {
			return nil, err
		}
		result[path] = checkpointTreeEntry{parts[2], parts[0]}
	}
	return result, nil
}
func (h *WorkspaceHistory) blob(entry checkpointTreeEntry) ([]byte, error) {
	if entry.OID == "" {
		return nil, nil
	}
	size, err := h.git(nil, "cat-file", "-s", entry.OID)
	if err != nil {
		return nil, err
	}
	var n int
	if _, err = fmt.Sscan(string(size), &n); err != nil || n > 1024*1024 {
		return nil, fmt.Errorf("rewind files must be at most 1 MiB")
	}
	if n == 0 {
		return []byte{}, nil
	}
	return h.git(nil, "cat-file", "blob", entry.OID)
}
func readRewindTarget(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return nil, fmt.Errorf("rewind target is non-regular or exceeds 1 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1024*1024 {
		return nil, fmt.Errorf("rewind target grew beyond 1 MiB")
	}
	return raw, nil
}
func writeRewindTarget(path string, data []byte, mode uint32, expected *string) error {
	current, err := readRewindTarget(path)
	if err != nil {
		return err
	}
	if !equalHash(rawDigest(current), expected) {
		return fmt.Errorf("file changed before rewind replacement")
	}
	if data == nil {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".rewind-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Chmod(tmp.Name(), os.FileMode(mode)); err != nil {
		return err
	}
	current, err = readRewindTarget(path)
	if err != nil {
		return err
	}
	if !equalHash(rawDigest(current), expected) {
		return fmt.Errorf("file changed during rewind replacement")
	}
	return replaceWorkspaceFile(tmp.Name(), path)
}
func rawDigest(data []byte) *string {
	if data == nil {
		return nil
	}
	sum := sha256sum(data)
	return &sum
}
func equalHash(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
func sha256sum(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Rewind is an explicit operator action. It never runs automatically on retry,
// never invokes git reset/checkout, and restores only the reviewed before/after diff.
func (h *WorkspaceHistory) Rewind(id, expectedAfter string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.active) > 0 {
		return fmt.Errorf("workspace has active attempts")
	}
	record, err := h.load(id)
	if err != nil {
		return err
	}
	if record.Outcome != "failed" || !record.FileOnly || record.Overlap || record.Rewound || record.After != expectedAfter {
		return fmt.Errorf("checkpoint is not an eligible failed file-only attempt or expected state changed")
	}
	patches, err := safeHistoryPath(h.workspace, "patches")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(patches, 0700); err != nil {
		return err
	}
	transaction, err := lockWorkspaceFile(filepath.Join(patches, "transaction.reticle-lock"))
	if err != nil {
		return fmt.Errorf("workspace patch transaction is busy: %w", err)
	}
	defer transaction.Close()
	pending, err := filepath.Glob(filepath.Join(patches, "*.json"))
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("pending patch recovery must finish before rewind")
	}
	before, err := h.tree(record.Before)
	if err != nil {
		return err
	}
	after, err := h.tree(record.After)
	if err != nil {
		return err
	}
	names := []string{}
	all := map[string]bool{}
	for name := range before {
		all[name] = true
	}
	for name := range after {
		all[name] = true
	}
	for name := range all {
		if before[name] != after[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 32 {
		return fmt.Errorf("rewind exceeds 32-file transaction budget")
	}
	if len(names) == 0 {
		record.Rewound = true
		now := time.Now()
		record.RewoundAt = &now
		return h.save(record)
	}
	journal := rewindJournal{CreatedAt: time.Now(), Operation: "rewind", Version: 1, State: "prepared", TaskID: record.TaskID, AttemptID: record.AttemptID, Files: []rewindFile{}}
	locks := []*os.File{}
	defer func() {
		for _, lock := range locks {
			lock.Close()
		}
	}()
	total := 0
	seenNames := map[string]bool{}
	for _, name := range names {
		if seenNames[strings.ToLower(name)] {
			return fmt.Errorf("case-aliased rewind targets are unsupported")
		}
		seenNames[strings.ToLower(name)] = true
		target, err := safeHistoryPath(h.source, name)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		lock, err := lockWorkspaceFile(target + ".reticle-lock")
		if err != nil {
			return fmt.Errorf("file is busy: %s", name)
		}
		locks = append(locks, lock)
		current, err := readRewindTarget(target)
		if err != nil {
			return err
		}
		expected := after[name]
		capturedAfter, readErr := h.blob(expected)
		if readErr != nil {
			return readErr
		}
		if (current == nil) != (expected.OID == "") || !bytes.Equal(current, capturedAfter) {
			return fmt.Errorf("file changed after the attempt; preserving it: %s", name)
		}
		original, err := h.blob(before[name])
		if err != nil {
			return err
		}
		if before[name].OID != "" && expected.OID != "" && before[name].Mode != expected.Mode {
			return fmt.Errorf("mode-only rewinds require separate review")
		}
		mode := uint32(0600)
		if current != nil {
			info, statErr := os.Stat(target)
			if statErr != nil {
				return statErr
			}
			mode = uint32(info.Mode().Perm()) & 0777
			currentExecutable := mode&0111 != 0
			if currentExecutable != (expected.Mode == "100755") {
				return fmt.Errorf("file executability changed after the attempt; preserving it: %s", name)
			}
		} else if before[name].Mode == "100755" {
			mode = 0700
		}

		total += len(current) + len(original)
		if total > 8*1024*1024 {
			return fmt.Errorf("rewind exceeds 8 MiB transaction budget")
		}
		journal.Files = append(journal.Files, rewindFile{Path: name, Before: current, After: original, BeforeHash: rawDigest(current), AfterHash: rawDigest(original), Mode: mode})
	}
	// Same journal format as patch_tools: interrupted operator rewinds are rolled
	// back by normal SDK patch recovery before later editing.
	journalPath := filepath.Join(patches, id+".json")
	if err = saveHistoryJSON(journalPath, journal); err != nil {
		return err
	}
	journal.State = "committing"
	if err = saveHistoryJSON(journalPath, journal); err != nil {
		return err
	}
	replace := writeRewindTarget
	if h.replaceFile != nil {
		replace = h.replaceFile
	}
	for _, entry := range journal.Files {
		target, err := safeHistoryPath(h.source, entry.Path)
		if err != nil {
			return err
		}
		current, err := readRewindTarget(target)
		if err != nil {
			return err
		}
		if !equalHash(rawDigest(current), entry.BeforeHash) {
			return fmt.Errorf("file changed during rewind; pending journal retained for recovery")
		}
		if err = replace(target, entry.After, entry.Mode, entry.BeforeHash); err != nil {
			return fmt.Errorf("rewind interrupted; pending journal retained for recovery: %w", err)
		}
	}
	journal.State = "committed"
	if err = saveHistoryJSON(journalPath, journal); err != nil {
		return err
	}
	completed, err := safeHistoryPath(h.workspace, "patches/completed")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(completed, 0700); err != nil {
		return err
	}
	if err = os.Rename(journalPath, filepath.Join(completed, filepath.Base(journalPath))); err != nil {
		return err
	}
	record.Rewound = true
	now := time.Now()
	record.RewoundAt = &now
	return h.save(record)
}
