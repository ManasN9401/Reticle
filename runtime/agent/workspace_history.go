package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

// Private session history never writes the user's repository index, refs or worktree metadata.
// Git fast-import consumes bounded raw data, bypassing project hooks, filters and .gitattributes.
type WorkspaceCheckpoint struct {
	ID        string     `json:"id"`
	TaskID    string     `json:"task_id"`
	AttemptID string     `json:"attempt_id"`
	Before    string     `json:"before"`
	After     string     `json:"after,omitempty"`
	Outcome   string     `json:"outcome"`
	FileOnly  bool       `json:"file_only"`
	Overlap   bool       `json:"overlap"`
	Rewound   bool       `json:"rewound,omitempty"`
	RewoundAt *time.Time `json:"rewound_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}
type workspaceBlob struct {
	Data []byte
	Mode uint32
}
type WorkspaceHistory struct {
	replaceFile              func(string, []byte, uint32, *string) error
	mu                       sync.Mutex
	workspace, source, store string
	owner                    *os.File
	active                   map[string]*WorkspaceCheckpoint
}

var workspaceHistories sync.Map
var workspaceHistoryOpen sync.Mutex

func safeHistoryPath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.ContainsAny(relative, "\\:\x00\r\n") {
		return "", fmt.Errorf("invalid workspace path")
	}
	relative = filepath.FromSlash(relative)
	clean := filepath.Clean(relative)
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		lower := strings.ToLower(strings.SplitN(part, ".", 2)[0])
		reserved := lower == "con" || lower == "prn" || lower == "aux" || lower == "nul" || (len(lower) == 4 && (strings.HasPrefix(lower, "com") || strings.HasPrefix(lower, "lpt")) && lower[3] >= '1' && lower[3] <= '9')
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || reserved {
			return "", fmt.Errorf("nonportable workspace path denied")
		}
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace path escapes root")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if !canonicalWorkspaceEqual(resolved, absolute) {
		return "", fmt.Errorf("aliased workspace root denied")
	}
	target := filepath.Join(absolute, clean)
	for current := target; current != absolute; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e != nil && !os.IsNotExist(e) {
			return "", e
		}
		if e == nil {
			actual, resolveErr := filepath.EvalSymlinks(current)
			if info.Mode()&os.ModeSymlink != 0 || resolveErr != nil || !canonicalWorkspaceEqual(actual, current) {
				return "", fmt.Errorf("aliased workspace path denied")
			}
		}
	}
	return target, nil
}

func OpenWorkspaceHistory(workspace string) (*WorkspaceHistory, error) {
	workspaceHistoryOpen.Lock()
	defer workspaceHistoryOpen.Unlock()
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	if existing, ok := workspaceHistories.Load(absolute); ok {
		return existing.(*WorkspaceHistory), nil
	}
	source, err := safeHistoryPath(absolute, "src")
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("checkpoint source directory unavailable")
	}
	store, err := safeHistoryPath(absolute, "history")
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(store, 0700); err != nil {
		return nil, err
	}
	owner, err := lockWorkspaceFile(filepath.Join(store, "owner.reticle-lock"))
	if err != nil {
		return nil, fmt.Errorf("workspace history belongs to another runtime: %w", err)
	}
	h := &WorkspaceHistory{workspace: absolute, source: source, store: store, owner: owner, active: map[string]*WorkspaceCheckpoint{}}
	repo := filepath.Join(store, "repository")
	if _, err = os.Stat(filepath.Join(repo, "HEAD")); os.IsNotExist(err) {
		if _, err = h.git(nil, "init", "--bare", "--object-format=sha1", repo); err != nil {
			owner.Close()
			return nil, err
		}
	}
	if err = os.MkdirAll(filepath.Join(store, "records"), 0700); err != nil {
		owner.Close()
		return nil, err
	}
	workspaceHistories.Store(absolute, h)
	return h, nil
}

func (h *WorkspaceHistory) git(input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	prefix := []string{"--no-pager", "--git-dir=" + filepath.Join(h.store, "repository"), "-c", "core.hooksPath=" + filepath.Join(h.store, "disabled-hooks"), "-c", "core.autocrlf=false", "-c", "core.quotePath=false"}
	cmd := exec.CommandContext(ctx, "git", append(prefix, args...)...)
	// Do not inherit GIT_DIR, global config, alternate indexes, SSH commands or credentials.
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		if !strings.HasPrefix(upper, "GIT_") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	cmd.Dir = h.store
	cmd.Stdin = bytes.NewReader(input)
	var out limitedOutput
	out.limit = 1024 * 1024
	var stderr limitedOutput
	stderr.limit = 4096
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("workspace Git %s failed: %w: %s", args[0], err, stderr.String())
	}
	if out.exceeded && args[0] != "diff" {
		return nil, fmt.Errorf("workspace Git output exceeds 1 MiB")
	}
	return out.Bytes(), nil
}

func ignoredHistoryPath(relative string) bool {
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		lower := strings.ToLower(part)
		if strings.HasSuffix(lower, ".reticle-lock") || strings.HasPrefix(lower, ".env") {
			return true
		}
		switch lower {
		case ".git", ".reticle", ".aws", ".ssh", ".venv", "venv", "node_modules", "__pycache__", ".reticle-go-cache", ".reticle-npm-cache", ".reticle-uv-cache":
			return true
		}
	}
	return false
}
func (h *WorkspaceHistory) readSource() (map[string]workspaceBlob, error) {
	result := map[string]workspaceBlob{}
	total := 0
	err := filepath.WalkDir(h.source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == h.source {
			return nil
		}
		relative, err := filepath.Rel(h.source, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if ignoredHistoryPath(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target, err := safeHistoryPath(h.source, relative)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := os.Stat(target)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 16*1024*1024 {
			return fmt.Errorf("checkpoint file unsupported or exceeds 16 MiB: %s", relative)
		}
		f, err := os.Open(target)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, 16*1024*1024+1))
		f.Close()
		if readErr != nil {
			return readErr
		}
		total += len(data)
		if len(data) > 16*1024*1024 || total > 64*1024*1024 || len(result) >= 1000 {
			return fmt.Errorf("checkpoint exceeds file/byte budget")
		}
		mode := uint32(0100644)
		if info.Mode()&0111 != 0 {
			mode = 0100755
		}
		result[relative] = workspaceBlob{data, mode}
		return nil
	})
	return result, err
}
func (h *WorkspaceHistory) snapshot(message string) (string, error) {
	files, err := h.readSource()
	if err != nil {
		return "", err
	}
	check, err := h.readSource()
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(files, check) {
		return "", fmt.Errorf("workspace changed while preparing checkpoint")
	}
	parentRaw, parentErr := h.git(nil, "rev-parse", "--verify", "--quiet", "refs/reticle/checkpoints")
	parent := ""
	if parentErr != nil {
		var exit *exec.ExitError
		if !errors.As(parentErr, &exit) || exit.ExitCode() != 1 {
			return "", parentErr
		}
	} else {
		parent = strings.TrimSpace(string(parentRaw))
		if len(parent) != 40 {
			return "", fmt.Errorf("invalid checkpoint parent")
		}
	}
	var stream bytes.Buffer
	fmt.Fprintf(&stream, "commit refs/reticle/checkpoints\ncommitter Reticle <reticle@localhost> %d +0000\ndata %d\n%s\n", time.Now().Unix(), len(message), message)
	if parent != "" {
		fmt.Fprintf(&stream, "from %s\n", parent)
	}
	stream.WriteString("deleteall\n")
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file := files[name]
		fmt.Fprintf(&stream, "M %o inline %s\ndata %d\n", file.Mode, quoteCheckpointPath(name), len(file.Data))
		stream.Write(file.Data)
		stream.WriteByte('\n')
	}
	stream.WriteString("\ndone\n")
	if _, err = h.git(stream.Bytes(), "fast-import", "--quiet", "--done"); err != nil {
		return "", err
	}
	raw, err := h.git(nil, "rev-parse", "refs/reticle/checkpoints")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}
func saveHistoryJSON(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".history-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(raw); err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceWorkspaceFile(temporary.Name(), path)
}
func (h *WorkspaceHistory) recordPath(id string) (string, error) {
	if len(id) != 32 {
		return "", fmt.Errorf("invalid checkpoint id")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", err
	}
	return safeHistoryPath(h.workspace, "history/records/"+id+".json")
}
func (h *WorkspaceHistory) save(record *WorkspaceCheckpoint) error {
	encoded, encodeErr := json.Marshal(record)
	if encodeErr != nil {
		return encodeErr
	}
	if len(encoded) > 8192 {
		return fmt.Errorf("checkpoint record exceeds 8 KiB")
	}
	path, err := h.recordPath(record.ID)
	if err != nil {
		return err
	}
	return saveHistoryJSON(path, record)
}
func (h *WorkspaceHistory) Begin(task, attempt string) (*WorkspaceCheckpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	before, err := h.snapshot("Before attempt " + attempt)
	if err != nil {
		return nil, err
	}
	raw := make([]byte, 16)
	if _, err = rand.Read(raw); err != nil {
		return nil, err
	}
	record := &WorkspaceCheckpoint{ID: hex.EncodeToString(raw), TaskID: task, AttemptID: attempt, Before: before, Outcome: "running", CreatedAt: time.Now(), Overlap: len(h.active) > 0}
	for _, active := range h.active {
		active.Overlap = true
		if err = h.save(active); err != nil {
			return nil, err
		}
	}
	if err = h.save(record); err != nil {
		return nil, err
	}
	h.active[record.ID] = record
	return record, nil
}
func (h *WorkspaceHistory) Finish(record *WorkspaceCheckpoint, outcome, stderr string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	defer delete(h.active, record.ID)
	after, err := h.snapshot("After attempt " + record.AttemptID)
	if err != nil {
		return err
	}
	record.After, record.Outcome = after, outcome
	proof := progressProof(stderr)
	record.FileOnly = proof == retrySafeNoEffects || proof == retrySafeFileEffects
	return h.save(record)
}
func (h *WorkspaceHistory) Records() ([]WorkspaceCheckpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(h.store, "records"))
	if err != nil {
		return nil, err
	}
	if len(entries) > 10000 {
		return nil, fmt.Errorf("history record retention limit reached")
	}
	records := []WorkspaceCheckpoint{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path, err := h.recordPath(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		data, err := readCheckpointRecord(path)
		if err != nil {
			return nil, err
		}
		if len(data) > 8192 {
			return nil, fmt.Errorf("invalid checkpoint record size")
		}
		var record WorkspaceCheckpoint
		if err = json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		if record.Outcome == "running" && h.active[record.ID] == nil {
			record.Outcome = "interrupted"
		}
		if record.ID != strings.TrimSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("checkpoint identity mismatch")
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt.After(records[j].CreatedAt) })
	return records, nil
}
func (h *WorkspaceHistory) load(id string) (*WorkspaceCheckpoint, error) {
	path, err := h.recordPath(id)
	if err != nil {
		return nil, err
	}
	data, err := readCheckpointRecord(path)
	if err != nil {
		return nil, err
	}
	var record WorkspaceCheckpoint
	if len(data) > 8192 {
		return nil, fmt.Errorf("invalid checkpoint record")
	}
	if err = json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	if record.ID != id {
		return nil, fmt.Errorf("checkpoint identity mismatch")
	}
	for _, sha := range []string{record.Before, record.After} {
		if len(sha) != 40 {
			return nil, fmt.Errorf("checkpoint is incomplete")
		}
		if _, err = hex.DecodeString(sha); err != nil {
			return nil, err
		}
	}
	return &record, nil
}
func (h *WorkspaceHistory) Diff(id string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	record, err := h.load(id)
	if err != nil {
		return "", err
	}
	raw, err := h.git(nil, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--stat", record.Before, record.After, "--")
	if err != nil {
		return "", err
	}
	detail, err := h.git(nil, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", "--unified=3", record.Before, record.After, "--")
	if err != nil {
		return "", err
	}
	if len(detail) > 512*1024 {
		return string(raw) + "\n[Diff exceeds 512 KiB; inspect the private checkpoint objects before rewinding.]", nil
	}
	return string(raw) + "\n" + string(detail), nil
}

// Close is used when a session is retired; process death releases the OS lease.
func (h *WorkspaceHistory) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.active) > 0 {
		return fmt.Errorf("workspace has active attempts")
	}
	workspaceHistories.Delete(h.workspace)
	return h.owner.Close()
}

func canonicalWorkspaceEqual(a, b string) bool {
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func quoteCheckpointPath(path string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, value := range []byte(path) {
		if value == '"' || value == '\\' {
			out.WriteByte('\\')
			out.WriteByte(value)
		} else if value >= 32 && value < 127 {
			out.WriteByte(value)
		} else {
			fmt.Fprintf(&out, "\\%03o", value)
		}
	}
	out.WriteByte('"')
	return out.String()
}

func readCheckpointRecord(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil {
		return nil, err
	}
	if len(data) > 8192 {
		return nil, fmt.Errorf("checkpoint record exceeds 8 KiB")
	}
	return data, nil
}
