package agent

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Run after the worker exits, using an independent deadline: its context may
// already be cancelled. Label selection is scoped to a random runtime owner.
func cleanupAttemptContainers(owner string, run func(context.Context, ...string) ([]byte, error)) error {
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(owner) {
		return fmt.Errorf("invalid attempt resource owner")
	}
	if run == nil {
		run = func(ctx context.Context, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, "docker", args...).Output()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := run(ctx, "ps", "-aq", "--no-trunc", "--filter", "label=reticle.owner="+owner)
	if err != nil {
		return fmt.Errorf("list owned containers: %w", err)
	}
	ids := strings.Fields(string(output))
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > 64 {
		return fmt.Errorf("owned container count exceeds cleanup limit")
	}
	valid := regexp.MustCompile(`^[a-f0-9]{64}$`)
	for _, id := range ids {
		if !valid.MatchString(id) {
			return fmt.Errorf("invalid owned container identity")
		}
	}
	_, err = run(ctx, append([]string{"rm", "-f"}, ids...)...)
	return err
}
