package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AwaitApproval uses a separate decision document, never status text in a plan.
// Only the trusted local control UI should write decisions; native execution is
// explicitly trusted host execution, not an OS security boundary.
func AwaitApproval(ctx context.Context, root string, req Task, log func(string)) error {
	if root == "" {
		return fmt.Errorf("approval root is not configured")
	}
	payload, err := json.Marshal(struct {
		Task   TaskID
		Inputs []TaskInput
		Prompt any
	}{req.ID, req.Inputs, req.Memory["user_prompt"]})
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
	content := []byte("# Approval required\n\nReview the exact task inputs below. The decision is stored separately.\n\n```json\n" + string(payload) + "\n```\n")
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
