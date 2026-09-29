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

    def test_rfc_filename_heading_and_roadmap_register_agree(self):
        roadmap = (ROOT / "docs/ROADMAP.md").read_text(encoding="utf-8")
        for path in sorted((ROOT / "docs/rfc").glob("RFC-*.md")):
            with self.subTest(path=path.relative_to(ROOT)):
                match = re.match(r"RFC-(\d{3})-", path.name)
                self.assertIsNotNone(match, "RFC filename must use RFC-NNN-title.md")
                number = match.group(1)
                text = path.read_text(encoding="utf-8")
                self.assertRegex(text, rf"(?m)^# RFC-{number}(?::|\b)")
                link = f"rfc/{path.name}"
                status = re.search(r"(?m)^status:\s*(\S+)\s*$", text)
                self.assertIsNotNone(status)
                rows = [line for line in roadmap.splitlines() if line.startswith("|") and f"]({link})" in line]
                self.assertEqual(len(rows), 1, "RFC must appear once in roadmap register table")
                row = rows[0]
                self.assertTrue(row.rstrip().endswith(f"`{status.group(1)}` |"), "roadmap status must match RFC frontmatter")

    def test_relative_markdown_links_resolve(self):
        link_pattern = re.compile(r"\[[^\]]*\]\(([^)]+)\)")
        scheme_pattern = re.compile(r"^[a-z][a-z0-9+.-]*:", re.IGNORECASE)
        for path in (ROOT / "docs").rglob("*.md"):
            text = path.read_text(encoding="utf-8")
            for line_number, line in enumerate(text.splitlines(), 1):
                for raw_target in link_pattern.findall(line):
                    target = raw_target.strip().split("#", 1)[0]
                    if not target or target.startswith("/") or scheme_pattern.match(target):
                        continue
                    resolved = path.parent / target.replace("%20", " ")
                    with self.subTest(path=path.relative_to(ROOT), line=line_number, target=raw_target):
                        self.assertTrue(resolved.exists(), "relative Markdown link target does not exist")


if __name__ == "__main__":
    unittest.main()
