package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxQuotedInput bounds how much of one input is copied into the readable part of a request.
const maxQuotedInput = 4000

// requestPreface is the human-readable part of an approval request: the goal and the text of
// each input, quoted line by line. The exact request follows it as JSON; Studio builds its
// plan view from that JSON, and this part is for anyone reading the file directly. Quoting
// with "> " keeps a code fence inside an input from being mistaken for the JSON block.
func requestPreface(req Task) string {
	var out strings.Builder
	out.WriteString("# Approval required\n\nReview what is being approved below. The decision is stored separately and is bound to the exact text of this file.\n\n")
	if prompt, ok := req.Memory["user_prompt"].(string); ok && strings.TrimSpace(prompt) != "" {
		out.WriteString("## Goal\n\n")
		out.WriteString(quoteLines(prompt, maxQuotedInput))
		out.WriteString("\n")
	}
	if len(req.Inputs) > 0 {
		out.WriteString("## Inputs\n\n")
	}
	for _, input := range req.Inputs {
		name := input.Name
		if name == "" {
			name = input.ArtifactID
		}
		fmt.Fprintf(&out, "### %s\n\n", strings.ReplaceAll(name, "\n", " "))
		text, ok := input.Data.(string)
		if !ok {
			if encoded, err := json.MarshalIndent(input.Data, "", "  "); err == nil {
				text = string(encoded)
			}
		}
		out.WriteString(quoteLines(text, maxQuotedInput))
		out.WriteString("\n")
	}
	out.WriteString("## Exact request\n\n")
	return out.String()
}

// quoteLines prefixes every line with "> ", keeping at most limit characters of text.
func quoteLines(text string, limit int) string {
	more := 0
	if runes := []rune(text); len(runes) > limit {
		more = len(runes) - limit
		text = string(runes[:limit])
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = "> " + line
	}
	quoted := strings.Join(lines, "\n") + "\n"
	if more > 0 {
		quoted += fmt.Sprintf("> … %d more characters are in the JSON below.\n", more)
	}
	return quoted
}

// awaitsHuman reports whether a worker is the approval checkpoint. A person decides when
// they decide, so this node is not bound by the task deadline other workers run under.
func awaitsHuman(w *Worker) bool {
	return w != nil && w.ID == "hitl-agent"
}

// AwaitApproval uses a separate decision document, never status text in a plan.
// Only the trusted local control UI should write decisions; native execution is
// explicitly trusted host execution, not an OS security boundary.
//
// It waits until a decision is written or ctx is cancelled (the run is killed). There is
// deliberately no expiry: a request used to lapse after 30 minutes, which discarded the
// approval of anyone who stepped away. An old decision still cannot authorize a later
// retry, because every attempt writes a new request file with its own identity and only
// the worker polling that exact file reads its decision.
func AwaitApproval(ctx context.Context, root string, req Task, log func(string)) error {
	if root == "" {
		return fmt.Errorf("approval root is not configured")
	}
	payload, err := json.Marshal(struct {
		Task   TaskID      `json:"task"`
		Inputs []TaskInput `json:"inputs"`
		Prompt any         `json:"prompt"`
		Action any         `json:"protected_action,omitempty"`
	}{req.ID, req.Inputs, req.Memory["user_prompt"], req.Parameters["protected_action"]})
	if err != nil {
		return err
	}
	hash := sha256.Sum256(payload)
	digest := hex.EncodeToString(hash[:])
	dir := filepath.Join(root, ".reticle", "approvals")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// Fresh request identity prevents an old approval authorizing a retry.
	target := filepath.Join(dir, fmt.Sprintf("approval_req_%s_%d.md", digest[:16], time.Now().UnixNano()))
	content := []byte(requestPreface(req) + "```json\n" + string(payload) + "\n```\n")
	if err = os.WriteFile(target, content, 0600); err != nil {
		return err
	}
	requestHash := sha256.Sum256(content)
	log("[TOOL] Checkpoint file created at: " + target)
	log("[UI_STATE: WAITING_HUMAN]")
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			b, err := os.ReadFile(target + ".decision.json")
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			var decision struct {
				Hash     string `json:"hash"`
				Decision string `json:"decision"`
				Feedback string `json:"feedback"`
			}
			if json.Unmarshal(b, &decision) != nil {
				return fmt.Errorf("invalid approval decision")
			}
			current, err := os.ReadFile(target)
			if err != nil {
				return err
			}
			if sha256.Sum256(current) != requestHash || decision.Hash != hex.EncodeToString(requestHash[:]) {
				return fmt.Errorf("approval plan changed")
			}
			if decision.Decision != "APPROVED" {
				return fmt.Errorf("approval rejected: %s", decision.Feedback)
			}
			log("[UI_STATE: RESUMED]")
			return nil
		}
	}
}
