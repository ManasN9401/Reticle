import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/"cmd/forge/compiler/lib"))
import history_tools
import forge_utils


class HistoryInspection(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.workspace=Path(self.temp.name)
        self.root=self.workspace/"src"
        self.root.mkdir()
        self.store=self.workspace/"history"/"repository"
        self.store.parent.mkdir()
        subprocess.run(["git","init","--bare","--object-format=sha1",str(self.store)],capture_output=True,check=True)
        files={"a.txt":b"old\r\n","deleted.txt":b"remove\n","empty.txt":b"","binary.bin":b"\0\1"}
        data=b"commit refs/reticle/checkpoints\ncommitter Reticle <reticle@localhost> 1700000000 +0000\ndata 7\nfixture\ndeleteall\n"
        for name,raw in files.items():
            data+=f'M 100644 inline "{name}"\ndata {len(raw)}\n'.encode()+raw+b"\n"
            (self.root/name).write_bytes(raw)
        subprocess.run(["git","--git-dir="+str(self.store),"fast-import","--quiet","--done"],input=data+b"\ndone\n",capture_output=True,check=True)
        self.sha=history_tools._git(self.workspace,["rev-parse","refs/reticle/checkpoints"]).decode().strip()
        self.id="a"*32
        records=self.store.parent/"records"
        records.mkdir()
        (records/(self.id+".json")).write_text(json.dumps({"id":self.id,"task_id":"task","before":self.sha}))

    def options(self): return {"checkpoint_id":self.id,"task_id":"task"}

    def test_real_git_status_and_paged_diff_are_read_only(self):
        (self.root/"a.txt").write_bytes(b"new\r\n")
        (self.root/"deleted.txt").unlink()
        (self.root/"added.txt").write_text("added\n")
        (self.root/".env").write_text("excluded fixture")
        (self.root/"node_modules").mkdir()
        (self.root/"node_modules/file").write_text("excluded fixture")
        with patch.dict(os.environ,{"GIT_DIR":"must-not-use-user-metadata"}):
            result=json.loads(history_tools.git_status(self.workspace,limit=1,**self.options()))
            self.assertEqual(result["total"],3)
            self.assertEqual(result["next_offset"],2)
            diff=history_tools.git_diff(self.workspace,"a.txt",**self.options())
        self.assertIn("-old",diff)
        self.assertIn("+new",diff)
        self.assertFalse((self.store/"index").exists())
        self.assertEqual(history_tools._git(self.workspace,["rev-parse","refs/reticle/checkpoints"]).decode().strip(),self.sha)
        self.assertEqual((self.root/"empty.txt").read_bytes(),b"")

    def test_foreign_task_invalid_ids_and_paths_are_refused(self):
        with self.assertRaises(PermissionError):
            history_tools.git_status(self.workspace,checkpoint_id=self.id,task_id="another-task")
        with self.assertRaises(ValueError):
            history_tools.git_status(self.workspace,checkpoint_id="../escape",task_id="task")
        with self.assertRaises(ValueError):
            history_tools.git_diff(self.workspace,"../escape",**self.options())

    def test_binary_diff_is_bounded_and_does_not_inline_bytes(self):
        (self.root/"binary.bin").write_bytes(b"\0\2")
        text=history_tools.git_diff(self.workspace,"binary.bin",**self.options())
        self.assertIn("binary",text)
        self.assertNotIn("\0",text)

    def test_status_preserves_valid_json_at_small_tool_budget(self):
        for index in range(100): (self.root/(f'{index:03d}'+"x"*80)).write_text("changed")
        with patch.object(forge_utils,"TOOL_RESULT_CHARS",2000):
            text=history_tools.git_status(self.workspace,**self.options())
        result=json.loads(text)
        self.assertLess(len(text),2000)
        self.assertIsNotNone(result["next_offset"])


if __name__=="__main__":unittest.main()
