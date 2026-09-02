# RFC 038: Human-in-the-Loop (HitL) Checkpoints

## 1. Overview
As HyperParallel scales to handle massively parallel agent workflows, blindly executing a large Directed Acyclic Graph (DAG) for destructive tasks (e.g., deployments, dropping databases, major refactors) introduces unacceptable risk.

This RFC defines the architecture for the **Human-in-the-Loop (`hitl-agent`) Checkpoint Node**, which provides granular control by suspending graph execution until explicit human authorization is granted. 

## 2. The "GitOps" File-Based Approval Mechanism
To decouple approval logic from the complexities of UI state management and to provide a rich IDE experience for reviewing massive architectural plans, the HitL agent utilizes a File-Based Checkpoint system.

### 2.1 Checkpoint Generation
When the `GraphEngine` schedules a `hitl-agent` node:
1. The worker parses the incoming payload.
2. It generates a uniquely identifiable markdown file: `d:\HyperParallel\runtime\checkpoints\approval_req_{task_id}.md`.
3. The file cleanly formats the upstream Architect's proposed plan, context files, and the original user prompt.
4. The file terminates with a strict authorization block:
```markdown
STATUS: PENDING
FEEDBACK: 
```

### 2.2 Execution Suspension & Resolution
- **Polling:** The `hitl-agent` enters a suspension state, firing a `[UI_STATE: WAITING_HUMAN]` telemetry event to update the dashboard. It polls the markdown file every 2 seconds.
- **Approval:** If the user edits the file to `STATUS: APPROVED`, the agent safely deletes the file and exits with Code `0`, allowing the DAG to proceed to the destructive action.
- **Rejection & Feedback:** If the user edits the file to `STATUS: REJECTED` and fills out the `FEEDBACK` block (which supports multi-line text), the agent captures the feedback, prints it to stdout for the orchestrator, and exits with Code `1`. This halts the DAG branch and bubbles the feedback back up for re-planning.

## 3. Architect Injection Rules (Auto-Approve)
To ensure the system remains autonomous for safe tasks but strictly guarded for dangerous ones, the `architect-agent`'s core system prompt enforces dynamic injection.

- **The Rule:** The Architect MUST inject a `hitl-agent` node immediately before any agent responsible for a high-risk or destructive action.
- **The Override:** If the user submits a task with the `-auto-approve` flag (or `auto-approve: true` in the JSON payload), the Architect is explicitly instructed to bypass the injection rule, enabling full autonomous execution. As a secondary safety net, if a `hitl-agent` is somehow executed while `auto-approve` is True, it will immediately self-resolve with Code `0` and bypass file generation.
