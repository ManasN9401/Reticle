package logger

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeArgsPreservesErrorMessage(t *testing.T) {
	args := safeArgs([]any{"error", errors.New("skill dependency is invalid")})
	out, _, _ := formatArgsForConsole("Skill registry failed", args)
	if !strings.Contains(out, "skill dependency is invalid") || strings.Contains(out, "map[]") {
		t.Fatalf("error diagnostic was lost: %s", out)
	}
}
