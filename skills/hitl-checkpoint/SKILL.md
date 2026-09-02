---
name: Human-in-the-Loop Approval
description: Standardized method for pausing execution and polling for human feedback.
---

# HitL Methodology

This skill implements a File-Based "GitOps" approval flow. Since direct UI buttons are temporarily restricted, the agent drops a Markdown checkpoint file in the user's workspace, waiting for them to natively edit and save the file in their IDE.

## 1. Bypass Logic
- The agent MUST check the task payload for the flag `auto-approve: true`.
- If `auto-approve` is True, the agent must instantly exit with success (Code 0) and avoid writing any files.

## 2. Checkpoint Generation
- Create the checkpoint at: `d:\Reticle\runtime\checkpoints\approval_req_{task_id}.md`
- The markdown file must contain a clear header: `# 🛑 HUMAN APPROVAL REQUIRED`
- It must clearly outline the proposed plan from the upstream node (passed via context files or payload).
- At the very bottom of the file, it must append the exact string block:
```markdown
## Authorization
Change PENDING to APPROVED to authorize execution. Change to REJECTED to halt execution and return feedback to the Architect.

STATUS: PENDING
FEEDBACK: 
```

## 3. Polling Loop
- The agent will use a `while True:` loop, sleeping for 2 seconds, checking the `STATUS:` line in the generated `.md` file.
- If it detects `STATUS: APPROVED`, it deletes the `.md` file and exits with Code 0.
- If it detects `STATUS: REJECTED`, it extracts the text following `FEEDBACK:`, prints it to stdout so the orchestrator can capture it, and exits with Code 1.
