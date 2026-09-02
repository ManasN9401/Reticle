# Core Agents Index

Reticle comes with a suite of built-in agents, each specialized in a distinct domain. These agents can be orchestrated together via Directed Acyclic Graphs (DAGs) to solve complex, multi-modal tasks.

## System Agents

| Agent ID | Description |
|---|---|
| **`architect-agent`** | The core compiler agent. Responsible for digesting the user's high-level goal and dynamically architecting a DAG of specialized worker agents to fulfill it. |
| **`scaffolder-agent`** | Provisions workspaces, initializes repositories, and sets up project boilerplate before specialized worker agents begin. |

## Specialized Worker Agents

| Agent ID | Description |
|---|---|
| **`coder-agent`** | General-purpose software engineering agent. Capable of writing, refactoring, and debugging code across various languages. |
| **`hermes-coder-agent`** | An advanced, highly-skilled self-improvising coder. It strictly follows a Read-Eval-Print Loop (REPL), executing terminal commands to test its own code and self-correcting until all requirements pass flawlessly. |
| **`graphify-agent`** | Audits codebases by generating semantic knowledge graphs. Integrates deeply with the `graphifyy` engine to map out "God Nodes" and surprising connections. |
| **`osint-agent`** | Open Source Intelligence analyst. Utilizes tools like `maigret`, `spiderfoot`, and `photon` for passive reconnaissance and data gathering. |
| **`quant-agent`** | Quantitative data analyst. Handles complex statistical modeling, empirical computation, and data science workflows using `pandas` and `scipy`. |
| **`writer-agent`** | Technical writing and documentation expert. Enforces rigorous quality standards for reports, methodologies, and summaries. |
| **`latex-agent`** | Academic typesetting specialist. Converts structured reports and mathematical formulas into production-ready LaTeX PDFs. |
| **`browser-agent`** | Autonomous web navigation agent. Capable of reading documentation, scraping dynamic sites, and executing headless browser workflows. |

## Testing Agents

| Agent ID | Description |
|---|---|
| **`mock_stress_tester`** | Internal agent used for validating orchestrator concurrency, rate limits, and DAG throughput during CI/CD testing. |

---

**Note on Skills:** Any of the specialized worker agents can be further constrained and augmented by attaching [Specialized Skills](./specialized_skills.md) to their `definition.yml`.
