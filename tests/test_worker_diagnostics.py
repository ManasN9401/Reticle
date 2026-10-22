import json
import unittest

from test_worker_awareness import Harness, tool_response


class WorkerDiagnostics(Harness):
    def test_detected_check_verifies_worker_output_and_reports_receipt(self):
        script=[tool_response("write_file",{"path":"tests/test_example.py","content":"import unittest\n"}),
                tool_response("run_diagnostics",{"kind":"test"}),
                tool_response("mark_task_complete",{"summary":"Verified test output"})]
        failure,errors,remaining,seen,summary=self.run_worker(script,
            "Create or update exactly these workspace files: tests/test_example.py.\n",
            terminal=lambda *args,**kwargs:"Exit code: 0\nRan 1 test in 0.01s\nOK")
        self.assertIsNone(failure,errors)
        self.assertEqual(remaining,0)
        replies=[json.loads(m["content"]) for m in seen["messages"] if m.get("role")=="tool" and m.get("name")=="run_diagnostics"]
        # Tool messages use call identity, so also inspect structured JSON by shape.
        if not replies:
            replies=[json.loads(m["content"]) for m in seen["messages"] if m.get("role")=="tool" and m.get("content","").startswith('{"ok":')]
        self.assertTrue(replies[0]["ok"],replies)
        self.assertTrue(replies[0]["receipt"].startswith("diagnostics/"))
        self.assertEqual(replies[0]["checked_file_count"], 1)
        self.assertTrue(summary)
