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
		Task      TaskID      `json:"task"`
		Inputs    []TaskInput `json:"inputs"`
		Prompt    any         `json:"prompt"`
		Action    any         `json:"protected_action,omitempty"`
		ExpiresAt time.Time   `json:"expires_at"`
	}{req.ID, req.Inputs, req.Memory["user_prompt"], req.Parameters["protected_action"], time.Now().UTC().Add(30 * time.Minute)})
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
			// Expiry is deliberately checked against the request file timestamp so
			// an old decision cannot authorize a later retry.
			if info, statErr := os.Stat(target); statErr == nil && time.Since(info.ModTime()) > 30*time.Minute {
				return fmt.Errorf("approval expired")
			}
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
