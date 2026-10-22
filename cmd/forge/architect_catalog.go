package main

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/reticle/runtime/agent"
	"github.com/reticle/runtime/toolbroker"
)

type architectAgentCatalogEntry struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	Runtime      agent.RuntimeType  `json:"runtime"`
	Inputs       []string           `json:"inputs"`
	Outputs      []string           `json:"outputs"`
	Skills       []string           `json:"skills"`
	Capabilities []agent.Capability `json:"capabilities"`
	MCPServers   []string           `json:"mcp_servers,omitempty"`
	ToolPolicies []string           `json:"tool_policies,omitempty"`
}

type architectSkillCatalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	UseWhen     string `json:"use_when,omitempty"`
	// InstructionsBytes is what the skill adds to an agent's prompt; zero means dependencies only.
	InstructionsBytes  int      `json:"instructions_bytes"`
	InstructionsPath   string   `json:"instructions_path,omitempty"`
	InstructionsSHA256 string   `json:"instructions_sha256,omitempty"`
	Version            string   `json:"version"`
	DependencyPolicy   string   `json:"dependency_policy"`
	Dependencies       []string `json:"dependencies"`
	EnvVars            []string `json:"env_vars"`
}

type architectToolCatalogEntry struct {
	ID          string `json:"id"`
	Capability  string `json:"capability,omitempty"`
	Description string `json:"description"`
	Constraint  string `json:"constraint,omitempty"`
}

type architectCatalog struct {
	Agents       []architectAgentCatalogEntry `json:"agents"`
	Skills       []architectSkillCatalogEntry `json:"skills"`
	Capabilities []string                     `json:"capabilities"`
	Tools        []architectToolCatalogEntry  `json:"tools"`
	MCPServers   []string                     `json:"mcp_servers"`
}

var compilerServiceAgents = map[agent.WorkerID]struct{}{
	"architect-agent": {}, "coder-agent": {}, "scaffolder-agent": {},
	"writer-agent": {}, "mock_stress_tester": {},
}

// buildArchitectCatalog serializes the registry the runtime actually loaded.
// The architect must not maintain a second, filesystem-derived view of agents
// and skills because that view inevitably drifts from dispatch reality.
func buildArchitectCatalog(reg *agent.Registry, brokerTools ...[]toolbroker.Descriptor) (string, error) {
	catalog := architectCatalog{
		Capabilities: []string{
			"workspace.read", "workspace.write", "network.public", "process.container",
			"process.native", "memory.execution", "graph.delegate", "image.local",
			"rag.local", "cloud.plan", "cloud.apply", "security.active", "gpu.use", "mcp.call", "browser.run",
		},
		Tools: []architectToolCatalogEntry{
			{ID: "read_diagnostics", Capability: "workspace.read", Description: "Read paged captured diagnostic output from a receipt owned by the current task."},
			{ID: "run_diagnostics", Capability: "process.container", Description: "Detect project tests, type checks, lint and build scripts; return structured errors and artifact-bound verification receipts.", Constraint: "Also requires workspace.read. Uses the command container unless native execution is explicitly enabled; unavailable to DevOps and pentest specialists. Missing toolchains and empty test suites do not count as success."},
			{ID: "read_file", Capability: "workspace.read", Description: "Read a workspace text file; long files come back in parts with an offset to continue from."},
			{ID: "list_dir", Capability: "workspace.read", Description: "List a workspace directory."},
			{ID: "search_codebase", Capability: "workspace.read", Description: "Search workspace text."},
			{ID: "find_files", Capability: "workspace.read", Description: "List workspace files matching a glob such as **/*.js."},
			{ID: "check_web_page", Capability: "workspace.read", Description: "Statically check a generated web page and its scripts for problems a browser would hit (classic scripts using import, bare module imports, missing files)."},
			{ID: "run_page", Capability: "browser.run", Description: "Load a page of the site in a sandboxed headless browser and report uncaught exceptions, console errors, failed requests, canvas and animation state and whether the screen is blank; saves a screenshot.", Constraint: "Grant to integration and QA nodes that verify a web page. The page may read public files but can transmit data in public requests; known private addresses and other local services are filtered, without an OS egress guarantee."},
			{ID: "write_file", Capability: "workspace.write", Description: "Create a workspace text file, or replace one it has just read with overwrite=true."},
			{ID: "replace_file_content", Capability: "workspace.write", Description: "Change one exact piece of an existing file; reports the closest lines when the text is not found."},
			{ID: "apply_edits", Capability: "workspace.write", Description: "Make several exact-text edits to one file in one call, all or nothing."},
			{ID: "move_path", Capability: "workspace.write", Description: "Move or rename a workspace file or folder."},
			{ID: "delete_path", Capability: "workspace.write", Description: "Delete a workspace file or folder."},
			{ID: "read_url", Capability: "network.public", Description: "Read public HTTP documentation."},
			{ID: "execute_terminal_command", Capability: "process.container", Description: "Run a shell command in the workspace and read its exit code and output: tests, scripts, installs.", Constraint: "Runs in a throwaway container with Python 3.12, pip and network access but no node, npm or git, unless both process.native and the user's native-execution setting are enabled. Grant it to nodes that must run something; a node that only writes files does not need it."},
			{ID: "remember", Capability: "memory.execution", Description: "Store execution-scoped JSON memory."},
			{ID: "remember_if_version", Capability: "memory.execution", Description: "Compare-and-set execution memory."},
			{ID: "delegate", Capability: "graph.delegate", Description: "Request work from a registered agent."},
			{ID: "generate_local_asset", Capability: "image.local", Description: "Generate an image through local ComfyUI; checkpoint, width and height are optional."},
			{ID: "generate_local_assets", Capability: "image.local", Description: "Generate up to 12 images through local ComfyUI in one tool call."},
			{ID: "resize_image", Capability: "image.local", Description: "Resize an image file, keeping its aspect ratio or cropping to fill."},
			{ID: "make_thumbnails", Capability: "image.local", Description: "Write a thumbnail for every image in a workspace directory."},
			{ID: "convert_image", Capability: "image.local", Description: "Re-encode an image as PNG, JPEG or WebP."},
			{ID: "crop_image", Capability: "image.local", Description: "Crop an image to a pixel box."},
			{ID: "web_search", Capability: "network.public", Description: "Search the web through Google (Serper), Brave or a SearXNG server, whichever has credentials configured, and get ranked results; Wikipedia works without a key when asked for by name.", Constraint: "Available through the maintained osint-agent worker, not a generic generated coding worker."},
			{ID: "fetch_pages", Capability: "network.public", Description: "Read up to 20 public pages in parallel and save each as a cited source with its URL, time and hash.", Constraint: "Available through the maintained osint-agent worker, not a generic generated coding worker."},
			{ID: "browse_web", Capability: "browser.run", Description: "Open a public website in a real headless browser, click through it and extract text, links, tables and selected elements.", Constraint: "Available through the maintained browser-agent worker. Read-only by default; stops at CAPTCHAs and access walls."},
			{ID: "market_quote", Capability: "network.public", Description: "Current quote and key ratios for listed symbols from Yahoo Finance.", Constraint: "Available through the maintained quant-agent worker, not a generic generated coding worker."},
			{ID: "price_history", Capability: "network.public", Description: "Adjusted price history for a symbol, saved as CSV.", Constraint: "Available through the maintained quant-agent worker."},
			{ID: "financial_statements", Capability: "network.public", Description: "Income statement, balance sheet and cash flow for a symbol, saved as CSV with margins and changes.", Constraint: "Available through the maintained quant-agent worker."},
			{ID: "technical_indicators", Capability: "network.public", Description: "SMA, EMA, RSI, MACD, Bollinger bands and ATR for a symbol, computed in code.", Constraint: "Available through the maintained quant-agent worker."},
			{ID: "risk_metrics", Capability: "network.public", Description: "Return, volatility, Sharpe, drawdown, beta and correlation for symbols.", Constraint: "Available through the maintained quant-agent worker."},
			{ID: "index_directory", Capability: "rag.local", Description: "Index workspace files into local RAG.", Constraint: "Available through the maintained rag-agent worker, not a generic generated coding worker."},
			{ID: "query_knowledge", Capability: "rag.local", Description: "Query the local RAG index.", Constraint: "Available through the maintained rag-agent worker, not a generic generated coding worker."},
			{ID: "remove_path_from_index", Capability: "rag.local", Description: "Remove a path from local RAG.", Constraint: "Available through the maintained rag-agent worker, not a generic generated coding worker."},
			{ID: "mark_task_complete", Description: "Finish only after declared outputs are written and verified."},
		},
	}

	agentDefinitions, skillDefinitions := reg.CatalogSnapshot()
	for id, def := range agentDefinitions {
		if _, internal := compilerServiceAgents[id]; internal {
			continue
		}
		catalog.Agents = append(catalog.Agents, architectAgentCatalogEntry{
			ID: string(id), Name: def.Name, Description: def.Description, Runtime: def.Runtime,
			Inputs: def.Inputs, Outputs: def.Outputs, Skills: def.Skills, Capabilities: def.Capabilities,
			MCPServers: def.MCPServers, ToolPolicies: def.BrokerPolicies,
		})
	}
	if len(brokerTools) > 0 {
		for _, descriptor := range brokerTools[0] {
			catalog.Tools = append(catalog.Tools, architectToolCatalogEntry{
				ID: descriptor.Name, Capability: descriptor.RequiredCapability,
				Description: descriptor.Description, Constraint: descriptor.RequiredPolicy,
			})
			if strings.HasPrefix(descriptor.RequiredPolicy, "mcp.server:") {
				catalog.MCPServers = append(catalog.MCPServers, strings.TrimPrefix(descriptor.RequiredPolicy, "mcp.server:"))
			}
		}
	}
	for _, def := range skillDefinitions {
		catalog.Skills = append(catalog.Skills, architectSkillCatalogEntry{
			ID: def.ID, Name: def.Name, Description: def.Description, UseWhen: def.UseWhen,
			InstructionsBytes: def.InstructionsBytes,
			InstructionsPath:  def.InstructionsPath, InstructionsSHA256: def.InstructionsSHA256, Version: def.Version,
			DependencyPolicy: def.DependencyPolicy, Dependencies: def.Dependencies, EnvVars: def.EnvVars,
		})
	}
	sort.Slice(catalog.Agents, func(i, j int) bool { return catalog.Agents[i].ID < catalog.Agents[j].ID })
	sort.Slice(catalog.Skills, func(i, j int) bool { return catalog.Skills[i].ID < catalog.Skills[j].ID })
	sort.Strings(catalog.Capabilities)
	sort.Slice(catalog.Tools, func(i, j int) bool { return catalog.Tools[i].ID < catalog.Tools[j].ID })
	sort.Strings(catalog.MCPServers)
	if len(catalog.MCPServers) > 0 {
		unique := catalog.MCPServers[:1]
		for _, value := range catalog.MCPServers[1:] {
			if value != unique[len(unique)-1] {
				unique = append(unique, value)
			}
		}
		catalog.MCPServers = unique
	}
	data, err := json.Marshal(catalog)
	return string(data), err
}
