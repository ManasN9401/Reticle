from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]


class DocumentationContracts(unittest.TestCase):
    def test_rfc_and_specification_metadata(self):
        files = list((ROOT / "docs/rfc").glob("*.md"))
        files.extend((ROOT / "docs/specifications").rglob("*.md"))
        for path in files:
            with self.subTest(path=path.relative_to(ROOT)):
                text = path.read_text(encoding="utf-8")
                self.assertTrue(text.startswith("---\n"), "missing YAML frontmatter")
                end = text.find("\n---", 4)
                self.assertNotEqual(end, -1, "unterminated YAML frontmatter")
                frontmatter = text[4:end]
                for field in ("status", "owner", "updated"):
                    self.assertRegex(frontmatter, rf"(?m)^{field}:\s*\S")
                if re.search(r"(?m)^status:\s*historical\s*$", frontmatter):
                    body = text[end + 4:]
                    self.assertNotRegex(body, r"(?im)^(?:status:|\*\*status:\*\*)", "historical RFC has a conflicting current-status label")

    def test_current_runtime_diagrams_use_current_protocol_terms(self):
        obsolete = re.compile(
            r"\b(?:WorkspaceCreated|DAGGenerated|WorkflowGenerated|AgentFinished|FileLockRequested|CommitToWorkspace|executor\.go|Tenacity exhausted)\b"
        )
        for path in (ROOT / "docs/diagrams/runtime").glob("*.md"):
            if path.name == "test_coverage_matrix.md":
                continue
            with self.subTest(path=path.relative_to(ROOT)):
                self.assertIsNone(obsolete.search(path.read_text(encoding="utf-8")))


if __name__ == "__main__":
    unittest.main()
