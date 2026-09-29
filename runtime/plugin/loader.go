package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/reticle/runtime/agent"
	"github.com/reticle/runtime/mcp"
	"github.com/reticle/runtime/toolbroker"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,119}$`)
var safeToolID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,119}$`)
var semverPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:[-+][A-Za-z0-9.-]+)?$`)

type loadedBundle struct {
	root        string
	manifest    Manifest
	agents      map[agent.WorkerID]agent.AgentDefinition
	skills      map[string]agent.SkillDefinition
	descriptors []toolbroker.Descriptor
	bindings    map[string]toolBinding
}

func loadBundle(root string) (*loadedBundle, error) {
	manifestPath := filepath.Join(root, ManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read plugin manifest: %w", err)
	}
	if len(data) > 1024*1024 {
		return nil, fmt.Errorf("plugin manifest exceeds 1 MiB")
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse plugin manifest: %w", err)
	}
	if !safeID.MatchString(manifest.ID) || !semverPattern.MatchString(manifest.Version) {
		return nil, fmt.Errorf("plugin id or version is invalid")
	}
	if len(manifest.Description) > 4096 || !satisfies(CurrentReticleVersion, manifest.Compatibility.ReticleVersion) {
		return nil, fmt.Errorf("plugin requires Reticle %q; running version is %s", manifest.Compatibility.ReticleVersion, CurrentReticleVersion)
	}
	bundle := &loadedBundle{
		root: root, manifest: manifest, agents: make(map[agent.WorkerID]agent.AgentDefinition),
		skills: make(map[string]agent.SkillDefinition), bindings: make(map[string]toolBinding),
	}
	seenPaths := make(map[string]bool)
	seenToolIDs := make(map[string]bool)
	seenToolNames := make(map[string]bool)
	for _, relative := range manifest.Skills {
		path, err := bundlePath(root, relative)
		if err != nil || seenPaths[path] {
			return nil, fmt.Errorf("invalid or duplicate skill path %q", relative)
		}
		seenPaths[path] = true
		definition, err := agent.ReadSkillDefinition(path)
		if err != nil {
			return nil, err
		}
		if _, duplicate := bundle.skills[definition.ID]; duplicate {
			return nil, fmt.Errorf("plugin repeats skill %s", definition.ID)
		}
		bundle.skills[definition.ID] = definition
	}
	for _, relative := range manifest.Agents {
		path, err := bundlePath(root, relative)
		if err != nil || seenPaths[path] {
			return nil, fmt.Errorf("invalid or duplicate agent path %q", relative)
		}
		seenPaths[path] = true
		definition, err := agent.ReadAgentDefinition(path)
		if err != nil {
			return nil, err
		}
		if _, duplicate := bundle.agents[definition.ID]; duplicate {
			return nil, fmt.Errorf("plugin repeats agent %s", definition.ID)
		}
		definition.BrokerPolicies = append(definition.BrokerPolicies, "plugin:"+manifest.ID)
		for index := range definition.Subscriptions {
			definition.Subscriptions[index].ID = manifest.ID + "__" + definition.Subscriptions[index].ID
		}
		bundle.agents[definition.ID] = definition
	}
	for _, tool := range manifest.Tools {
		if !safeToolID.MatchString(tool.ID) || !safeID.MatchString(tool.Name) || strings.TrimSpace(tool.Description) == "" {
			return nil, fmt.Errorf("plugin tool identity is invalid")
		}
		if seenToolIDs[tool.ID] || seenToolNames[tool.Name] {
			return nil, fmt.Errorf("plugin repeats tool id or name %s", tool.ID)
		}
		seenToolIDs[tool.ID], seenToolNames[tool.Name] = true, true
		if !agent.IsKnownCapability(agent.Capability(tool.RequiredCapability)) {
			return nil, fmt.Errorf("plugin tool %s requires unknown capability %s", tool.ID, tool.RequiredCapability)
		}
		if tool.Effect != toolbroker.NoEffect && tool.Effect != toolbroker.EffectStarted && tool.Effect != toolbroker.Uncertain {
			return nil, fmt.Errorf("plugin tool %s has invalid effect certainty", tool.ID)
		}
		var schema map[string]any
		if len(tool.Schema) == 0 || len(tool.Schema) > 64*1024 || json.Unmarshal(tool.Schema, &schema) != nil || schema["type"] != "object" {
			return nil, fmt.Errorf("plugin tool %s requires an object JSON schema", tool.ID)
		}
		if tool.TimeoutMS == 0 {
			tool.TimeoutMS = 30_000
		}
		if tool.OutputLimit == 0 {
			tool.OutputLimit = 1024 * 1024
		}
		if tool.TimeoutMS < 1 || tool.TimeoutMS > 7_200_000 || tool.OutputLimit < 1 || tool.OutputLimit > 1024*1024 {
			return nil, fmt.Errorf("plugin tool %s has invalid limits", tool.ID)
		}
		resolvedCommand, err := resolvePluginCommand(root, tool.Command)
		if err != nil {
			return nil, fmt.Errorf("plugin tool %s: %w", tool.ID, err)
		}
		tool.Command = resolvedCommand
		for index, argument := range tool.Args {
			tool.Args[index] = strings.ReplaceAll(argument, "{pluginRoot}", root)
		}
		canonical := "plugin." + manifest.ID + "." + tool.ID
		bundle.descriptors = append(bundle.descriptors, toolbroker.Descriptor{
			ID: canonical, Name: tool.Name, Description: tool.Description, Schema: append(json.RawMessage(nil), tool.Schema...),
			RequiredCapability: tool.RequiredCapability, RequiredPolicy: "plugin:" + manifest.ID,
			TimeoutMS: tool.TimeoutMS, OutputLimit: tool.OutputLimit, Effect: tool.Effect, Available: true,
		})
		bundle.bindings[canonical] = toolBinding{pluginID: manifest.ID, root: root, manifest: tool}
	}
	seenMCPServers := make(map[string]bool)
	for index := range bundle.manifest.MCPServers {
		config := &bundle.manifest.MCPServers[index]
		if seenMCPServers[config.ID] {
			return nil, fmt.Errorf("plugin repeats MCP server %s", config.ID)
		}
		seenMCPServers[config.ID] = true
		config.Enabled = true
		if config.Transport == "" {
			config.Transport = "stdio"
		}
		if resolved, err := resolvePluginCommand(root, config.Command); err == nil {
			config.Command = resolved
		} else {
			return nil, fmt.Errorf("plugin MCP server %s: %w", config.ID, err)
		}
		if config.CWD == "" {
			config.CWD = root
		} else if !filepath.IsAbs(config.CWD) {
			path, err := bundleDirectory(root, config.CWD)
			if err != nil {
				return nil, fmt.Errorf("plugin MCP server %s has invalid cwd", config.ID)
			}
			config.CWD = path
		}
		for argIndex, argument := range config.Args {
			config.Args[argIndex] = strings.ReplaceAll(argument, "{pluginRoot}", root)
		}
		validated, err := mcp.ValidateServerConfig(root, *config)
		if err != nil {
			return nil, fmt.Errorf("plugin MCP server %s: %w", config.ID, err)
		}
		*config = validated
	}
	seenDependencies := make(map[string]bool)
	for _, dependency := range manifest.Dependencies {
		if !safeID.MatchString(dependency.Plugin) || strings.TrimSpace(dependency.VersionRange) == "" || dependency.Plugin == manifest.ID || seenDependencies[dependency.Plugin] {
			return nil, fmt.Errorf("plugin dependency is invalid")
		}
		seenDependencies[dependency.Plugin] = true
	}
	sort.Slice(bundle.descriptors, func(i, j int) bool { return bundle.descriptors[i].ID < bundle.descriptors[j].ID })
	return bundle, nil
}

func bundlePath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.ContainsRune(relative, 0) {
		return "", fmt.Errorf("bundle path must be relative")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("bundle path escapes plugin root")
	}
	target := filepath.Join(root, clean)
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("bundle path must name a regular non-symlink file")
	}
	return target, nil
}

func bundleDirectory(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.ContainsRune(relative, 0) {
		return "", fmt.Errorf("bundle directory must be relative")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("bundle directory escapes plugin root")
	}
	target := filepath.Join(root, clean)
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("bundle directory must name a non-symlink directory")
	}
	return target, nil
}

func resolvePluginCommand(root, command string) (string, error) {
	command = strings.ReplaceAll(strings.TrimSpace(command), "{pluginRoot}", root)
	if command == "" || strings.ContainsRune(command, 0) {
		return "", fmt.Errorf("command is required")
	}
	if filepath.IsAbs(command) {
		return "", fmt.Errorf("absolute commands are not portable plugin payloads")
	}
	if strings.ContainsAny(command, `/\`) || strings.HasPrefix(command, ".") {
		path, err := bundlePath(root, command)
		if err != nil {
			return "", err
		}
		return path, nil
	}
	// Bare interpreter/package-runner names resolve through the inherited safe
	// PATH; arguments can refer to {pluginRoot} explicitly.
	return command, nil
}

func satisfies(version, versionRange string) bool {
	versionRange = strings.TrimSpace(versionRange)
	if versionRange == "" || versionRange == "*" {
		return true
	}
	current, ok := parseVersion(version)
	if !ok {
		return false
	}
	for _, clause := range strings.Fields(strings.ReplaceAll(versionRange, ",", " ")) {
		operator := "="
		value := clause
		for _, candidate := range []string{">=", "<=", ">", "<", "^", "~", "="} {
			if strings.HasPrefix(clause, candidate) {
				operator, value = candidate, strings.TrimSpace(strings.TrimPrefix(clause, candidate))
				break
			}
		}
		target, valid := parseVersion(value)
		if !valid {
			return false
		}
		comparison := compareVersion(current, target)
		switch operator {
		case "=":
			if comparison != 0 {
				return false
			}
		case ">=":
			if comparison < 0 {
				return false
			}
		case "<=":
			if comparison > 0 {
				return false
			}
		case ">":
			if comparison <= 0 {
				return false
			}
		case "<":
			if comparison >= 0 {
				return false
			}
		case "^":
			compatible := current[0] == target[0]
			if target[0] == 0 {
				compatible = current[1] == target[1]
				if target[1] == 0 {
					compatible = current[2] == target[2]
				}
			}
			if comparison < 0 || !compatible {
				return false
			}
		case "~":
			if comparison < 0 || current[0] != target[0] || current[1] != target[1] {
				return false
			}
		}
	}
	return true
}

func parseVersion(value string) ([3]int, bool) {
	match := semverPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return [3]int{}, false
	}
	var result [3]int
	for index := range result {
		parsed, err := strconv.Atoi(match[index+1])
		if err != nil {
			return [3]int{}, false
		}
		result[index] = parsed
	}
	return result, true
}

func compareVersion(left, right [3]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
