package agent

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Agent-specific Python packages live in "libs" directories next to the base environment.
//
// They used to be named after the agent and installed with `pip --target`, which ignores what the
// base environment already has: every generated agent (a new name on every run) got its own full
// copy of litellm and its dependency tree, about 130 MB each. Thirty-one agents in one afternoon
// made four gigabytes. Now:
//   - packages the base environment provides are never installed again;
//   - what remains is shared by every agent that needs the same set (the directory is named by
//     the set, not by the agent);
//   - directories that have not been used for a while, or that exceed a size budget, are removed.

const (
	libsDirPrefix = "libs-"
	// A directory used this recently is never pruned, even over the size budget: a worker started
	// within the window may still have it on its PYTHONPATH.
	libsProtectedFor  = 6 * time.Hour
	defaultLibsMaxAge = 14 * 24 * time.Hour
	defaultLibsMaxGB  = 2.0
	lastUsedFile      = ".last-used"
)

// browserRunSkill is what the browser.run capability needs, whichever skills an agent was given:
// the Playwright driver, preferred by run_page when importable. It is optional because run_page
// has a built-in fallback, so an install that cannot complete must not fail the node.
var browserRunSkill = SkillDefinition{
	ID:                   "browser-run",
	Name:                 "Browser driver for run_page",
	DependencyPolicy:     "locked",
	OptionalDependencies: []string{"playwright==1.63.0"},
}

// capabilitySkills are the implicit dependency groups of an agent's capabilities.
func capabilitySkills(capabilities []Capability) []SkillDefinition {
	var skills []SkillDefinition
	if hasCapability(capabilities, CapabilityBrowserRun) {
		skills = append(skills, browserRunSkill)
	}
	return skills
}

// optionalRetryAfter is how long a failed optional install is left alone before it is tried again,
// so a machine with no network does not pay for the attempt on every node.
const optionalRetryAfter = time.Hour

// ensureOptional installs optional packages into target, best-effort. It returns whether target
// can be put on the path. A failure is recorded and never returned as an error.
func ensureOptional(target string, dependencies []string, install func(target string, dependencies []string) ([]byte, error), now time.Time) (bool, string) {
	wanted := strings.Join(dependencies, "\n")
	if existing, err := os.ReadFile(filepath.Join(target, ".deps")); err == nil && string(existing) == wanted {
		return true, ""
	}
	failedMarker := filepath.Join(target, ".failed")
	if info, err := os.Stat(failedMarker); err == nil && now.Sub(info.ModTime()) < optionalRetryAfter {
		return false, "an install failed recently and is not retried yet"
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		return false, err.Error()
	}
	output, err := install(target, dependencies)
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 400 {
			message = message[len(message)-400:]
		}
		_ = os.WriteFile(failedMarker, []byte(err.Error()+"\n"+message), 0600)
		return false, err.Error()
	}
	_ = os.Remove(failedMarker)
	if err := os.WriteFile(filepath.Join(target, ".deps"), []byte(wanted), 0600); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// Directories from before the set-based naming: "<agent>-<16 hex>-libs".
var legacyLibsName = regexp.MustCompile(`^.+-[0-9a-f]{16}-libs$`)

var requirementName = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._-]*)`)

// requirementKey is the comparable name of a requirement: "Requests[socks]>=2" -> "requests".
func requirementKey(requirement string) string {
	match := requirementName.FindStringSubmatch(requirement)
	if len(match) != 2 {
		return ""
	}
	return strings.ToLower(strings.NewReplacer("_", "-", ".", "-").Replace(match[1]))
}

// extraDependencies returns the requirements the base environment does not already provide,
// de-duplicated and in a stable order. A skill that lists litellm or requests adds nothing:
// the base environment pins them, and the install's constraints file would reject a conflict.
func extraDependencies(dependencies, baseDependencies []string) []string {
	provided := map[string]bool{}
	for _, dependency := range baseDependencies {
		provided[requirementKey(dependency)] = true
	}
	seen := map[string]bool{}
	var extras []string
	for _, dependency := range dependencies {
		dependency = strings.TrimSpace(dependency)
		key := requirementKey(dependency)
		if dependency == "" || key == "" || provided[key] || seen[dependency] {
			continue
		}
		seen[dependency] = true
		extras = append(extras, dependency)
	}
	sort.Strings(extras)
	return extras
}

// sharedLibsPath names the directory for one set of extra requirements on one interpreter.
func (em *EnvironmentManager) sharedLibsPath(pythonExe, pythonVersion, constraints string, extras []string) string {
	identity := strings.Join([]string{pythonExe, pythonVersion, runtime.GOOS + "/" + runtime.GOARCH, constraints, strings.Join(extras, "\n")}, "\n")
	digest := sha256.Sum256([]byte(identity))
	return filepath.Join(em.BaseDir, fmt.Sprintf("%s%x", libsDirPrefix, digest[:8]))
}

func touchLibs(path string) {
	_ = os.WriteFile(filepath.Join(path, lastUsedFile), []byte(time.Now().UTC().Format(time.RFC3339)), 0600)
}

// directoryUse is when a libs directory was last used: its marker, else its install record.
func directoryUse(path string) time.Time {
	var newest time.Time
	for _, name := range []string{lastUsedFile, ".deps"} {
		if info, err := os.Stat(filepath.Join(path, name)); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if newest.IsZero() {
		if info, err := os.Stat(path); err == nil {
			newest = info.ModTime()
		}
	}
	return newest
}

func directorySize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// pruneLibs removes libs directories under baseDir that are unused beyond maxAge, then the least
// recently used until the total fits maxBytes. The base environment, the directory named in keep
// and anything used within libsProtectedFor are never removed. It returns what it removed.
func pruneLibs(baseDir, keep string, now time.Time, maxAge time.Duration, maxBytes int64) []string {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}
	type candidate struct {
		path   string
		used   time.Time
		size   int64
		legacy bool
	}
	var candidates []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		legacy := legacyLibsName.MatchString(name)
		if !legacy && !strings.HasPrefix(name, libsDirPrefix) {
			continue
		}
		path := filepath.Join(baseDir, name)
		if path == keep {
			continue
		}
		candidates = append(candidates, candidate{path: path, used: directoryUse(path), legacy: legacy})
	}
	var removed []string
	remove := func(c candidate) {
		if err := os.RemoveAll(c.path); err == nil {
			removed = append(removed, filepath.Base(c.path))
		}
	}
	var kept []candidate
	for _, c := range candidates {
		age := now.Sub(c.used)
		switch {
		case age < libsProtectedFor:
			kept = append(kept, c)
		case c.legacy && age >= 24*time.Hour:
			// Never read again by this version; only the age guards a concurrent older build.
			remove(c)
		case maxAge > 0 && age >= maxAge:
			remove(c)
		default:
			kept = append(kept, c)
		}
	}
	if maxBytes > 0 {
		var total int64
		sizes := map[string]int64{}
		for i := range kept {
			kept[i].size = directorySize(kept[i].path)
			sizes[kept[i].path] = kept[i].size
			total += kept[i].size
		}
		if keep != "" {
			total += directorySize(keep)
		}
		sort.Slice(kept, func(i, j int) bool { return kept[i].used.Before(kept[j].used) })
		for _, c := range kept {
			if total <= maxBytes {
				break
			}
			if now.Sub(c.used) < libsProtectedFor {
				continue
			}
			remove(c)
			total -= sizes[c.path]
		}
	}
	return removed
}

// pruneLibsOnce applies the retention policy once per process, tunable with
// RETICLE_ENV_CACHE_MAX_GB (0 disables the size budget) and RETICLE_ENV_CACHE_MAX_AGE_DAYS
// (0 disables the age limit).
func (em *EnvironmentManager) pruneLibsOnce(keep string) {
	em.pruneOnce.Do(func() {
		maxAge := defaultLibsMaxAge
		if value, err := strconv.ParseFloat(os.Getenv("RETICLE_ENV_CACHE_MAX_AGE_DAYS"), 64); err == nil && value >= 0 {
			maxAge = time.Duration(value * float64(24*time.Hour))
		}
		maxGB := defaultLibsMaxGB
		if value, err := strconv.ParseFloat(os.Getenv("RETICLE_ENV_CACHE_MAX_GB"), 64); err == nil && value >= 0 {
			maxGB = value
		}
		removed := pruneLibs(em.BaseDir, keep, time.Now(), maxAge, int64(maxGB*1024*1024*1024))
		if len(removed) > 0 && em.Logger != nil {
			em.Logger.Info("Removed unused agent dependency folders", "count", len(removed), "folders", strings.Join(removed, ","))
		}
	})
}
