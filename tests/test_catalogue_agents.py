"""Every agent the architect can pick must have a worker that does what its description says.

browser-agent, quant-agent, osint-agent and hermes-coder-agent were once registered with a worker
that merely ran the code generator (coder.py), while their descriptions promised Playwright,
yfinance, Google search and a read-eval-print loop. The architect chose them on those promises and
got a generic coder. This test fails when any catalogue agent's entrypoint is that shim again."""
import re
import unittest
from pathlib import Path

AGENTS = Path(__file__).resolve().parents[1] / "cmd/forge/compiler/agents"

# The agents whose job is to generate other agents or plans: running a generator is what they are for.
GENERATORS = {"coder-agent", "scaffolder-agent", "architect-agent", "mock_stress_tester"}

# What a specialist's worker must show: it runs the shared worker loop for its own kind of work.
SPECIALIST_KINDS = {
    "browser-agent": "browser", "osint-agent": "osint", "quant-agent": "quant", "hermes-coder-agent": "hermes",
    "devops-agent": "devops", "pentester-agent": "pentest", "rag-agent": "rag",
}


def definitions():
    for path in sorted(AGENTS.glob("*/definition.y*ml")):
        text = path.read_text(encoding="utf-8")
        entry = re.search(r"^entrypoint:\s*(\S+)", text, re.M)
        yield path.parent.name, text, (path.parent / entry.group(1)) if entry else None


class CatalogueAgentsHaveWorkers(unittest.TestCase):
    def test_no_agent_is_a_shell_around_the_code_generator(self):
        shells = []
        for name, _, worker in definitions():
            if name in GENERATORS or worker is None or not worker.is_file():
                continue
            source = worker.read_text(encoding="utf-8")
            if "coder-agent/workers/coder.py" in source or re.search(r"runpy\.run_path\([^)]*coder", source):
                shells.append(name)
        self.assertEqual(shells, [], "these agents run the code generator instead of doing their own work")

    def test_each_specialist_runs_the_worker_loop_for_its_own_kind(self):
        for name, kind in SPECIALIST_KINDS.items():
            with self.subTest(agent=name):
                worker = next(w for n, _, w in definitions() if n == name)
                source = worker.read_text(encoding="utf-8")
                self.assertRegex(source, r"from worker_sdk import run")
                self.assertRegex(source, r"run\(instructions,\s*['\"]" + kind + r"['\"]")

    def test_a_specialist_declares_the_skill_and_capabilities_its_tools_need(self):
        needs = {
            "browser-agent": ("browser-automation", {"network.public", "browser.run"}),
            "osint-agent": ("osint-investigation", {"network.public"}),
            "quant-agent": ("quant-finance", {"network.public"}),
            "hermes-coder-agent": ("hermes-repl", {"process.container"}),
        }
        by_name = {name: text for name, text, _ in definitions()}
        for name, (skill, capabilities) in needs.items():
            with self.subTest(agent=name):
                text = by_name[name]
                self.assertIn(f"  - {skill}\n", text)
                for capability in capabilities:
                    self.assertIn(f"  - {capability}\n", text)
                self.assertTrue((AGENTS.parents[3] / "skills" / f"{skill}.yaml").is_file())

    def test_descriptions_do_not_promise_what_the_worker_lacks(self):
        # The promise and the tool that keeps it are named together, so deleting a tool fails here.
        promises = {
            "browser-agent": ("Playwright", "browse_web"),
            "osint-agent": ("search", "web_search"),
            "quant-agent": ("yfinance", "market_quote"),
        }
        library = (AGENTS.parent / "lib" / "worker_sdk.py").read_text(encoding="utf-8")
        by_name = {name: text for name, text, _ in definitions()}
        for name, (word, tool) in promises.items():
            with self.subTest(agent=name):
                self.assertIn(word.lower(), by_name[name].lower())
                self.assertIn(f'definitions["{tool}"]', library)


if __name__ == "__main__":
    unittest.main()
