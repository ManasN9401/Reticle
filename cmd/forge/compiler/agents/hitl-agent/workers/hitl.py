"""Compatibility marker: checkpoints are executed by the Go runtime.

The runtime intercepts hitl-agent and waits for a hash-bound decision created
through the authenticated Studio checkpoint API. Direct execution cannot approve.
"""
raise SystemExit("Run hitl-agent through Reticle; direct Python approval is unsupported")
