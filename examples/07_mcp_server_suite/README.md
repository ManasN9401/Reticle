# MCP server suite

This example provides four dependency-free local MCP servers for exercising
Reticle's RFC-046 stdio adapter. They implement the modern `2026-07-28`
`server/discover` protocol and expose only bounded, deterministic tools.

| Reticle server ID | Script | Purpose | Tools |
| --- | --- | --- | --- |
| `text-tools` | `text_server.py` | Text analysis and transformation | `analyze_text`, `transform_text` |
| `math-tools` | `math_server.py` | Numerical summaries and sequences | `descriptive_statistics`, `fibonacci` |
| `planning-tools` | `planning_server.py` | Checklists and risk assessment | `create_checklist`, `assess_risks` |
| `workspace-reader` | `workspace_server.py` | Bounded read-only workspace access | `list_files`, `read_text` |

Run the local verification harness before connecting them to Reticle:

```powershell
cd C:\Users\nathm\code\Reticle\examples\07_mcp_server_suite
python .\verify_servers.py
```

## Studio connection settings

Add each entry under **Settings → Extensions → MCP Servers**. All four use:

- command: `python`
- transport: `stdio`
- working directory: `C:\Users\nathm\code\Reticle`
- startup timeout: `10`
- call timeout: `15`
- environment mappings: none
- enabled: yes

Each server has one argument, the absolute script path shown below:

```text
text-tools       C:\Users\nathm\code\Reticle\examples\07_mcp_server_suite\text_server.py
math-tools       C:\Users\nathm\code\Reticle\examples\07_mcp_server_suite\math_server.py
planning-tools   C:\Users\nathm\code\Reticle\examples\07_mcp_server_suite\planning_server.py
workspace-reader C:\Users\nathm\code\Reticle\examples\07_mcp_server_suite\workspace_server.py
```

`connections.json` contains the same information as complete server objects for
reference. Do not copy it directly over `.reticle/mcp/servers.json` while Forge
is running; Studio or the authenticated runtime API remains the source of truth.

The workspace reader resolves all paths beneath its configured working
directory, rejects traversal outside it, never writes files, caps listings at
500 files, and caps text reads at 100,000 characters.
