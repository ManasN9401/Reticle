package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRequirementKeyNormalisesNames(t *testing.T) {
	for requirement, want := range map[string]string{
		"litellm":                      "litellm",
		"LiteLLM==1.99.0":              "litellm",
		"requests[socks]>=2.3":         "requests",
		"Pillow":                       "pillow",
		"typing_extensions":            "typing-extensions",
		"zope.interface>=5":            "zope-interface",
		"  numpy ; python_version>'3'": "numpy",
		"":                             "",
		"==1.0":                        "",
	} {
		if got := requirementKey(requirement); got != want {
			t.Errorf("requirementKey(%q) = %q, want %q", requirement, got, want)
		}
	}
}

// Observed: [litellm requests Pillow] installed litellm's whole dependency tree again for each of
// 31 agents. The base environment pins litellm and requests; only Pillow is missing.
func TestPackagesTheBaseProvidesAreNotInstalledAgain(t *testing.T) {
	base := []string{"litellm==1.99.0", "requests==2.34.2", "tenacity==9.1.4"}
	got := extraDependencies([]string{"litellm", "requests", "Pillow"}, base)
	if !reflect.DeepEqual(got, []string{"Pillow"}) {
		t.Fatalf("got %v", got)
	}
	if got := extraDependencies([]string{"litellm", "Requests>=2", "tenacity"}, base); len(got) != 0 {
		t.Fatalf("a skill that only lists base packages needs nothing installed: %v", got)
	}
	// Stable and de-duplicated, so the same set always names the same folder.
	a := extraDependencies([]string{"torch==2.3", "Pillow", "Pillow", "numpy"}, base)
	b := extraDependencies([]string{"numpy", "Pillow", "torch==2.3"}, base)
	if !reflect.DeepEqual(a, b) || len(a) != 3 {
		t.Fatalf("not stable: %v vs %v", a, b)
	}
}

func TestAgentsNeedingTheSameSetShareOneFolder(t *testing.T) {
	em := &EnvironmentManager{BaseDir: filepath.Join(os.TempDir(), "envs")}
	one := em.sharedLibsPath("py", "3.12", "c", []string{"Pillow"})
	if again := em.sharedLibsPath("py", "3.12", "c", []string{"Pillow"}); again != one {
		t.Fatal("the same set got a different folder")
	}
	for name, other := range map[string]string{
		"another set":         em.sharedLibsPath("py", "3.12", "c", []string{"Pillow", "numpy"}),
		"another interpreter": em.sharedLibsPath("py", "3.13", "c", []string{"Pillow"}),
		"other constraints":   em.sharedLibsPath("py", "3.12", "c2", []string{"Pillow"}),
	} {
		if other == one {
			t.Errorf("%s shared a folder", name)
		}
	}
	if !strings.HasPrefix(filepath.Base(one), libsDirPrefix) {
		t.Fatalf("unexpected name %s", one)
	}
}

func libsFolder(t *testing.T, base, name string, size int, used time.Time) string {
	t.Helper()
	path := filepath.Join(base, name)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "package.bin"), make([]byte, size), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, lastUsedFile)
	if err := os.WriteFile(marker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(marker, used, used); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestPruneRemovesOldFoldersAndKeepsRecentOnes(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	old := libsFolder(t, base, "libs-aaaaaaaaaaaaaaaa", 10, now.Add(-30*24*time.Hour))
	recent := libsFolder(t, base, "libs-bbbbbbbbbbbbbbbb", 10, now.Add(-2*24*time.Hour))
	active := libsFolder(t, base, "libs-cccccccccccccccc", 10, now.Add(-90*24*time.Hour))
	baseEnv := filepath.Join(base, "forge-base")
	if err := os.MkdirAll(baseEnv, 0755); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(base, "notes")
	if err := os.MkdirAll(unrelated, 0755); err != nil {
		t.Fatal(err)
	}

	removed := pruneLibs(base, active, now, 14*24*time.Hour, 0)
	if !reflect.DeepEqual(removed, []string{"libs-aaaaaaaaaaaaaaaa"}) {
		t.Fatalf("removed %v", removed)
	}
	if exists(old) || !exists(recent) || !exists(active) || !exists(baseEnv) || !exists(unrelated) {
		t.Fatal("pruning touched something it should not have")
	}
}

// Observed: 31 folders of about 130 MB. Over the size budget the least recently used go first,
// while anything used in the last few hours stays even if that leaves the folder over budget.
func TestPruneEnforcesTheSizeBudgetLeastRecentlyUsedFirst(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	oldest := libsFolder(t, base, "libs-1111111111111111", 400, now.Add(-5*24*time.Hour))
	middle := libsFolder(t, base, "libs-2222222222222222", 400, now.Add(-3*24*time.Hour))
	newest := libsFolder(t, base, "libs-3333333333333333", 400, now.Add(-24*time.Hour))
	justUsed := libsFolder(t, base, "libs-4444444444444444", 400, now.Add(-time.Hour))

	removed := pruneLibs(base, "", now, 0, 1000)
	if exists(oldest) || exists(middle) {
		t.Fatalf("the least recently used folders should go first (removed %v)", removed)
	}
	if !exists(newest) && !exists(justUsed) {
		t.Fatal("pruned past the budget")
	}
	if !exists(justUsed) {
		t.Fatal("a folder used within the protected window was removed")
	}
}

func TestPruneNeverRemovesAFolderUsedWithinTheProtectedWindow(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	fresh := libsFolder(t, base, "libs-5555555555555555", 5000, now.Add(-10*time.Minute))
	pruneLibs(base, "", now, time.Hour, 1)
	if !exists(fresh) {
		t.Fatal("a folder a running worker may still be using was removed")
	}
}

func TestLegacyAgentNamedFoldersAreMigratedAway(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	oldLegacy := libsFolder(t, base, "scene-core-agent-0123456789abcdef-libs", 10, now.Add(-2*24*time.Hour))
	freshLegacy := libsFolder(t, base, "qa-test-agent-fedcba9876543210-libs", 10, now.Add(-time.Hour))
	pruneLibs(base, "", now, 0, 0)
	if exists(oldLegacy) {
		t.Fatal("a folder from the per-agent naming, unused for a day, should have been removed")
	}
	if !exists(freshLegacy) {
		t.Fatal("a recently used legacy folder may belong to a run on an older build")
	}
}

// A package the worker has a fallback for must never be a reason the worker cannot start: Playwright
// is preferred by run_page but the built-in client works without it.
func TestOptionalInstallIsBestEffortAndRetriedSlowly(t *testing.T) {
	target := filepath.Join(t.TempDir(), "libs-opt")
	calls := 0
	failing := func(string, []string) ([]byte, error) {
		calls++
		return []byte("ERROR: no matching distribution found for playwright"), os.ErrNotExist
	}
	now := time.Now()
	if ok, reason := ensureOptional(target, []string{"playwright==1.63.0"}, failing, now); ok || reason == "" {
		t.Fatalf("a failed install must report why and not be usable: ok=%v reason=%q", ok, reason)
	}
	// A recent failure is not paid for again by the next node.
	if ok, _ := ensureOptional(target, []string{"playwright==1.63.0"}, failing, now.Add(10*time.Minute)); ok || calls != 1 {
		t.Fatalf("retried too soon: ok=%v calls=%d", ok, calls)
	}
	// After the retry window it is tried again, and a success is remembered.
	succeeding := func(dir string, packages []string) ([]byte, error) {
		calls++
		return nil, os.WriteFile(filepath.Join(dir, "playwright.py"), []byte("x"), 0600)
	}
	if ok, _ := ensureOptional(target, []string{"playwright==1.63.0"}, succeeding, now.Add(2*time.Hour)); !ok || calls != 2 {
		t.Fatalf("not retried after the window: ok=%v calls=%d", ok, calls)
	}
	if exists(filepath.Join(target, ".failed")) {
		t.Fatal("a success left its failure marker behind")
	}
	if ok, _ := ensureOptional(target, []string{"playwright==1.63.0"}, failing, now.Add(3*time.Hour)); !ok || calls != 2 {
		t.Fatalf("an installed set was installed again: ok=%v calls=%d", ok, calls)
	}
	// A different set is a different install.
	if ok, _ := ensureOptional(target, []string{"playwright==1.64.0"}, succeeding, now.Add(3*time.Hour)); !ok || calls != 3 {
		t.Fatalf("a changed set was not installed: ok=%v calls=%d", ok, calls)
	}
}

func TestBrowserRunBringsItsDriverWhateverSkillsTheAgentHas(t *testing.T) {
	withBrowser := capabilitySkills([]Capability{CapabilityWorkspaceRead, CapabilityBrowserRun})
	if len(withBrowser) != 1 || len(withBrowser[0].OptionalDependencies) != 1 || !strings.HasPrefix(withBrowser[0].OptionalDependencies[0], "playwright==") {
		t.Fatalf("browser.run did not bring the driver: %+v", withBrowser)
	}
	if len(withBrowser[0].Dependencies) != 0 {
		t.Fatal("the driver must be optional, never a required dependency")
	}
	if got := capabilitySkills([]Capability{CapabilityWorkspaceRead, CapabilityWorkspaceWrite}); len(got) != 0 {
		t.Fatalf("an agent without browser.run gained packages: %+v", got)
	}
}

func TestOptionalDependenciesAreValidatedLikeRequiredOnes(t *testing.T) {
	write := func(body string) string {
		path := filepath.Join(t.TempDir(), "skill.yaml")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good, err := ReadSkillDefinition(write("id: x\nname: x\ndependency_policy: floating\noptional_dependencies:\n  - playwright\n"))
	if err != nil || len(good.OptionalDependencies) != 1 {
		t.Fatalf("a valid optional dependency was rejected: %v %+v", err, good)
	}
	for name, body := range map[string]string{
		"unsafe name":        "id: x\nname: x\noptional_dependencies:\n  - \"evil; rm -rf /\"\n",
		"unpinned in locked": "id: x\nname: x\ndependency_policy: locked\noptional_dependencies:\n  - playwright\n",
		"in a profile skill": "id: x\nname: x\ndependency_policy: profile\noptional_dependencies:\n  - playwright\n",
	} {
		if _, err := ReadSkillDefinition(write(body)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
