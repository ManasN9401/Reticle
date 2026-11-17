package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAttemptContainerCleanupScopesAndValidatesResources(t *testing.T) {
	owner := strings.Repeat("a", 32)
	id := strings.Repeat("b", 64)
	calls := [][]string{}
	run := func(ctx context.Context, args ...string) ([]byte, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("cleanup needs an independent deadline")
		}
		calls = append(calls, args)
		if len(calls) == 1 {
			return []byte(id + "\n"), nil
		}
		return nil, nil
	}
	if err := cleanupAttemptContainers(owner, run); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"ps", "-aq", "--no-trunc", "--filter", "label=reticle.owner=" + owner}, {"rm", "-f", id}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v", calls)
	}
	for _, bad := range []string{"", "--all", strings.Repeat("g", 32)} {
		if err := cleanupAttemptContainers(bad, run); err == nil {
			t.Fatalf("accepted invalid owner %q", bad)
		}
	}
}

func TestAttemptContainerCleanupNeverWidensSelection(t *testing.T) {
	for _, output := range []string{"", "--all", strings.Repeat("a", 64) + " --force"} {
		calls := 0
		err := cleanupAttemptContainers(strings.Repeat("c", 32), func(context.Context, ...string) ([]byte, error) {
			calls++
			return []byte(output), nil
		})
		if calls != 1 {
			t.Fatalf("unexpected removal for %q", output)
		}
		if (err == nil) != (output == "") {
			t.Fatalf("output=%q err=%v", output, err)
		}
	}
	expected := errors.New("docker unavailable")
	err := cleanupAttemptContainers(strings.Repeat("c", 32), func(context.Context, ...string) ([]byte, error) { return nil, expected })
	if !errors.Is(err, expected) {
		t.Fatalf("lost cleanup failure: %v", err)
	}
}
