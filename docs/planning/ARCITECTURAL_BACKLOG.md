# Architectural Backlog

This document records architectural ideas that have broad agreement but have not
yet been incorporated into permanent documentation.

The purpose is to preserve design intent while avoiding unnecessary delays in
writing formal documents.

---

## RFC-000

### Reasoned Evolution

The project should encourage architectural evolution based on evidence rather
than preserving existing designs for their own sake.

Architectural values should remain relatively stable.

Architectural mechanisms are expected to evolve.

Future RFCs should justify significant architectural changes by explaining:

- the problem being solved,
- why the current approach is insufficient,
- expected benefits,
- trade-offs.

Status: Agreed

---

## External Integrations

### MCP and API Support

While the Skeleton Runtime currently uses simple local scripts, the architecture must support true AI agents (LLMs) and Model Context Protocol (MCP) servers. The standard JSON-RPC STDIO IPC mechanism guarantees that any agent, whether a simple python script or a massive LLM acting through an MCP server, can be seamlessly plugged into the runtime. 

Status: Planned for future milestones.