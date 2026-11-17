import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/"cmd/forge/compiler/lib"))
import patch_tools
from test_worker_awareness import Harness, tool_response


def update(name, old, new):
    return f"--- a/{name}\n+++ b/{name}\n@@ -1 +1 @@\n-{old}\n+{new}\n"


class Patches(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.workspace=self.temp.name
        self.root=Path(self.workspace)/"src"
        self.root.mkdir()
        (self.root/"a.txt").write_text("old a\n")
        (self.root/"b.txt").write_text("old b\n")
        self.diff=update("a.txt","old a","new a")+update("b.txt","old b","new b")

    def test_multi_file_add_update_delete_and_receipt(self):
        diff=update("a.txt","old a","new a")+"--- /dev/null\n+++ b/new/file.txt\n@@ -0,0 +1 @@\n+created\n"+"--- a/b.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-old b\n"
        report=patch_tools.apply_patch(diff,self.workspace,task_id="task",attempt_id="attempt")
        self.assertTrue(report["ok"])
        self.assertEqual((self.root/"a.txt").read_text(),"new a\n")
        self.assertEqual((self.root/"new/file.txt").read_text(),"created\n")
        self.assertFalse((self.root/"b.txt").exists())
        record=json.loads((Path(self.workspace)/report["receipt"]).read_text())
        self.assertEqual(record["state"],"committed")
        self.assertEqual(record["task_id"],"task")
        self.assertEqual(patch_tools.pending(self.workspace),[])

    def test_invalid_second_file_leaves_first_unchanged(self):
        with self.assertRaisesRegex(ValueError,"context"):
            patch_tools.apply_patch(update("a.txt","old a","new a")+update("b.txt","absent","new b"),self.workspace)
        self.assertEqual((self.root/"a.txt").read_text(),"old a\n")
        self.assertEqual((self.root/"b.txt").read_text(),"old b\n")

    def test_crlf_and_missing_final_newline(self):
        (self.root/"a.txt").write_bytes(b"old a\r\ncontext\r\n")
        patch_tools.apply_patch(update("a.txt","old a","new a"),self.workspace)
        self.assertEqual((self.root/"a.txt").read_bytes(),b"new a\r\ncontext\r\n")
        (self.root/"b.txt").write_bytes(b"old b")
        patch_tools.apply_patch("--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-old b\n\\ No newline at end of file\n+new b\n\\ No newline at end of file\n",self.workspace)
        self.assertEqual((self.root/"b.txt").read_bytes(),b"new b")

    def test_unique_whitespace_match_preserves_context(self):
        (self.root/"a.txt").write_text("  context\n    old a\n")
        patch_tools.apply_patch("--- a/a.txt\n+++ b/a.txt\n@@ -1,2 +1,2 @@\n context\n-old a\n+new a\n",self.workspace)
        self.assertEqual((self.root/"a.txt").read_text(),"  context\nnew a\n")

    def test_ambiguous_context_is_not_guessed(self):
        (self.root/"a.txt").write_text("old a\nold a\n")
        with self.assertRaisesRegex(ValueError,"ambiguous"):
            patch_tools.apply_patch("--- a/a.txt\n+++ b/a.txt\n@@ -50 +50 @@\n-old a\n+new a\n",self.workspace)

    def test_commit_io_failure_restores_all_originals(self):
        original=patch_tools._replace
        calls=0
        def fail_second(target,raw,mode,**options):
            nonlocal calls
            calls+=1
            if calls==2: raise OSError("fixture write failed")
            return original(target,raw,mode,**options)
        with patch.object(patch_tools,"_replace",side_effect=fail_second):
            with self.assertRaisesRegex(OSError,"fixture write failed"):
                patch_tools.apply_patch(self.diff,self.workspace)
        self.assertEqual((self.root/"a.txt").read_text(),"old a\n")
        self.assertEqual((self.root/"b.txt").read_text(),"old b\n")
        self.assertEqual(patch_tools.pending(self.workspace),[])

    def crash(self):
        script="""import os,sys
sys.path.insert(0,sys.argv[1])
import patch_tools
original=patch_tools._replace
calls=0
def replace(target,raw,mode,**options):
 global calls
 original(target,raw,mode,**options)
 calls+=1
 if calls==1: os._exit(23)
patch_tools._replace=replace
patch_tools.apply_patch(sys.argv[3],sys.argv[2])
"""
        result=subprocess.run([sys.executable,"-B","-c",script,str(Path(patch_tools.__file__).parent),self.workspace,self.diff],capture_output=True)
        self.assertEqual(result.returncode,23,result.stderr)

    def test_process_death_recovers_partial_commit(self):
        self.crash()
        self.assertEqual((self.root/"a.txt").read_text(),"new a\n")
        self.assertEqual(len(patch_tools.pending(self.workspace)),1)
        self.assertEqual(len(patch_tools.recover(self.workspace)),1)
        self.assertEqual((self.root/"a.txt").read_text(),"old a\n")
        self.assertEqual((self.root/"b.txt").read_text(),"old b\n")
        self.assertEqual(patch_tools.recover(self.workspace),[])

    def test_recovery_does_not_overwrite_later_user_change(self):
        self.crash()
        (self.root/"b.txt").write_text("user edit\n")
        with self.assertRaisesRegex(ValueError,"conflicts"):
            patch_tools.recover(self.workspace)
        self.assertEqual((self.root/"a.txt").read_text(),"new a\n")
        self.assertEqual((self.root/"b.txt").read_text(),"user edit\n")
        with self.assertRaisesRegex(ValueError,"conflicts"):
            patch_tools.apply_patch(update("a.txt","new a","later"),self.workspace)

    def test_paths_counts_and_aliases_are_checked(self):
        for diff in (update("../escape","a","b"),update(".git/config","a","b"),update(".git./config","a","b"),update("NUL.txt","a","b"),
                     self.diff+update("a.txt","a","b"),update("a","a","b")+update("a/b","a","b"),
                     "--- a/a.txt\n+++ b/a.txt\n@@ -1,2 +1 @@\n-old a\n+new a\n"):
            with self.subTest(diff=diff):
                with self.assertRaises(ValueError): patch_tools.apply_patch(diff,self.workspace)
        self.assertEqual((self.root/"a.txt").read_text(),"old a\n")

    def test_concurrent_edit_during_temporary_write_is_preserved(self):
        chmod=patch_tools.os.chmod
        def concurrent(path,mode):
            (self.root/"a.txt").write_text("user concurrent edit\n")
            return chmod(path,mode)
        with patch.object(patch_tools.os,"chmod",side_effect=concurrent):
            with self.assertRaisesRegex(ValueError,"conflicts"):
                patch_tools.apply_patch(self.diff,self.workspace)
        self.assertEqual((self.root/"a.txt").read_text(),"user concurrent edit\n")
        self.assertEqual((self.root/"b.txt").read_text(),"old b\n")

    def test_src_alias_is_normalized_once_and_recovery_paths_are_literal(self):
        (self.root/"src").mkdir()
        (self.root/"src/file.txt").write_text("before\n")
        report=patch_tools.apply_patch(update("src/src/file.txt","before","after"),self.workspace)
        self.assertEqual(report["files"][0]["path"],"src/file.txt")
        self.assertEqual((self.root/"src/file.txt").read_text(),"after\n")
        self.assertFalse((self.root/"file.txt").exists())
        record=patch_tools._load_record(self.workspace,Path(self.workspace)/report["receipt"])
        self.assertEqual(record["files"][0]["path"],"src/file.txt")

    def test_context_scan_is_bounded(self):
        with patch.object(patch_tools,"MAX_CONTEXT_SCAN",0):
            with self.assertRaisesRegex(ValueError,"scan budget"):
                patch_tools.apply_patch("--- a/a.txt\n+++ b/a.txt\n@@ -40 +40 @@\n-old a\n+new a\n",self.workspace)
        self.assertEqual((self.root/"a.txt").read_text(),"old a\n")

    def test_html_duplicate_document_guard_is_retained(self):
        (self.root/"index.html").write_text("<!doctype html>\n")
        with self.assertRaisesRegex(ValueError,"duplicate HTML"):
            patch_tools.apply_patch("--- a/index.html\n+++ b/index.html\n@@ -1 +1,2 @@\n <!doctype html>\n+<!doctype html>\n",self.workspace)


class PatchWorker(Harness):
    def test_delete_only_patch_has_a_verified_deletion_receipt(self):
        diff="--- a/a.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-old a\n"
        script=[tool_response("apply_patch",{"patch":diff}),tool_response("mark_task_complete",{"summary":"deleted"})]
        failure,errors,remaining,_,summary=self.run_worker(script,"Delete a.txt",existing={"a.txt":"old a\n"})
        self.assertIsNone(failure,errors)
        self.assertEqual(remaining,0)
        self.assertEqual(summary,"deleted")

    def test_patch_outputs_require_fresh_verification(self):
        script=[tool_response("apply_patch",{"patch":update("a.txt","old a","new a")+update("b.txt","old b","new b")}),
                tool_response("mark_task_complete",{"summary":"premature"}),
                tool_response("read_file",{"path":"a.txt"}),tool_response("read_file",{"path":"b.txt"}),
                tool_response("mark_task_complete",{"summary":"verified"})]
        failure,errors,remaining,seen,summary=self.run_worker(script,"Create or update exactly these workspace files: a.txt, b.txt.\n",existing={"a.txt":"old a\n","b.txt":"old b\n"})
        self.assertIsNone(failure,errors)
        self.assertEqual(remaining,0)
        self.assertEqual(summary,"verified")
        self.assertIn("[RETICLE_EFFECTS: FILES]",errors)
        self.assertTrue(any("No successful verification" in m.get("content","") for m in seen["messages"] if m["role"]=="tool"))


if __name__=="__main__": unittest.main()
