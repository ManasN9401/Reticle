package agent

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

func cleanupExecutionContainers(id string) {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(id) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label=reticle.execution="+id).Output()
	if err != nil {
		return
	}
	for _, container := range strings.Fields(string(output)) {
		if regexp.MustCompile(`^[a-f0-9]{12,64}$`).MatchString(container) {
			exec.CommandContext(ctx, "docker", "rm", "-f", container).Run()
		}
	}
}
