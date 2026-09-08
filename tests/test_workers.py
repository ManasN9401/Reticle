import ast
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import io
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parents[1]
LIB = ROOT / "cmd/forge/compiler/lib"
sys.path.insert(0, str(LIB))
import forge_utils

class WorkerContracts(unittest.TestCase):
    def test_generated_workers_and_workflow(self):
        for agent, file in [("coder-agent","coder.py"),("hermes-coder-agent","hermes.py"),("quant-agent","quant.py"),("osint-agent","osint.py"),("browser-agent","browser.py"),("scaffolder-agent","scaffolder.py")]:
            with self.subTest(agent=agent), tempfile.TemporaryDirectory() as temp:
                dag={"agents":[{"id":"fixture","is_new":True,"system_prompt":"A quote: ' and newline\nfixture","skills":[]}],"nodes":[{"id":"node","agent_id":"fixture","modality":"coding","parameters":{"effort":"high"}}],"edges":[]}
                req={"id":"compile-exec-test|generate","execution":"compile-exec-test","memory":{"workspace_dir":temp},"inputs":[{"name":"DAG JSON","data":json.dumps(dag)}]}
                proc=subprocess.run([sys.executable,"-B",str(ROOT/"cmd/forge/compiler/agents"/agent/"workers"/file)],input=json.dumps(req),text=True,capture_output=True,timeout=10)
                self.assertEqual(proc.returncode,0,proc.stderr)
                self.assertEqual(json.loads(proc.stdout)["id"],req["id"])
                for generated in Path(temp).rglob("*.py"):
                    ast.parse(generated.read_text(encoding="utf-8"))
                if agent=="scaffolder-agent":
                    wf=json.loads(next(Path(temp).rglob("workflow_*.yaml")).read_text())
                    self.assertEqual(wf["nodes"][0]["parameters"],{"effort":"high"})
                    self.assertEqual(wf["nodes"][0]["modality"],"coding")

    def test_containment_and_literal_preservation(self):
        with tempfile.TemporaryDirectory() as temp:
            for path in ("../escape", "C:/escape", "/escape", "..\\escape"):
                self.assertTrue(forge_utils.write_file(path,"fixture",temp,{}).startswith("Error"))
            content='value = "\\n"\n'
            self.assertTrue(forge_utils.write_file("literal.py",content,temp,{}).startswith("Successfully"))
            self.assertEqual((Path(temp)/"src/literal.py").read_text(),content)
            self.assertTrue(forge_utils.replace_file_content("literal.py",'"\\n"','"\\t"',temp).startswith("Successfully"))
            ast.parse((Path(temp)/"src/literal.py").read_text())

    def test_url_scheme(self):
        for url in ("file:///etc/passwd","http://127.0.0.1/","http://localhost/"):
            self.assertTrue(forge_utils.read_url(url,".").startswith("Error"))

    def test_sdk_verification_and_memory(self):
        import worker_sdk
        def response(name, args):
            call=SimpleNamespace(id="call", function=SimpleNamespace(name=name,arguments=json.dumps(args)))
            message=SimpleNamespace(tool_calls=[call], model_dump=lambda **kwargs: {"role":"assistant","tool_calls":[{"id":"call","type":"function","function":{"name":name,"arguments":json.dumps(args)}}]})
            return SimpleNamespace(choices=[SimpleNamespace(message=message)])
        calls=[response("mark_task_complete",{"summary":"premature"}),response("list_dir",{"path":"."}),response("remember",{"key":"fixture","value_json":"42"}),response("mark_task_complete",{"summary":"verified"})]
        with tempfile.TemporaryDirectory() as temp:
            req={"id":"execution|node","execution":"execution","memory":{"workspace_dir":temp},"parameters":{"llm_model":"ollama/fixture"}}
            output=io.StringIO()
            def completion(**kwargs):
                self.assertEqual(kwargs["num_retries"],0)
                self.assertEqual(kwargs["api_base"],"http://localhost:11434")
                return calls.pop(0)
            with patch.dict(sys.modules,{"litellm":SimpleNamespace(completion=completion)}),patch.dict("os.environ",{},clear=True),patch.object(sys,"stdin",io.StringIO(json.dumps(req))),patch.object(sys,"stdout",output):
                worker_sdk.run("Fixture",kind="writing")
            result=json.loads(output.getvalue())
            self.assertEqual(result["artifact"]["data"],"verified")
            self.assertEqual(result["memory"],[{"key":"fixture","value":42,"scope":"execution"}])
            self.assertEqual(calls,[])

    def test_sdk_exhaustion_is_failure(self):
        import worker_sdk
        message=SimpleNamespace(tool_calls=[],model_dump=lambda **kwargs:{"role":"assistant","content":"done"})
        count=[]
        def completion(**kwargs):
            count.append(1)
            return SimpleNamespace(choices=[SimpleNamespace(message=message)])
        req={"id":"fixture","memory":{"workspace_dir":"unused"},"parameters":{"llm_model":"ollama/fixture"}}
        with patch.dict(sys.modules,{"litellm":SimpleNamespace(completion=completion)}),patch.object(sys,"stdin",io.StringIO(json.dumps(req))),patch.object(sys,"stdout",io.StringIO()) as output:
            with self.assertRaisesRegex(RuntimeError,"budget exhausted"):
                worker_sdk.run("Fixture")
            self.assertEqual(output.getvalue(),"")
            self.assertEqual(len(count),30)

    def test_sdk_does_not_retry_after_file_effect(self):
        import worker_sdk
        for mutate in (False,True):
            with self.subTest(mutate=mutate),tempfile.TemporaryDirectory() as temp:
                req={"id":"fixture","memory":{"workspace_dir":temp},"parameters":{"llm_model":"ollama/fixture"}}
                count=[]
                def completion(**kwargs):
                    count.append(1)
                    if mutate and len(count)==1:
                        call=SimpleNamespace(id="write",function=SimpleNamespace(name="write_file",arguments=json.dumps({"path":"fixture.txt","content":"data"})))
                        message=SimpleNamespace(tool_calls=[call],model_dump=lambda **kwargs:{"role":"assistant"})
                        return SimpleNamespace(choices=[SimpleNamespace(message=message)])
                    raise RuntimeError("RateLimitError")
                with patch.dict(sys.modules,{"litellm":SimpleNamespace(completion=completion)}),patch.object(sys,"stdin",io.StringIO(json.dumps(req))),patch.object(sys,"stderr",io.StringIO()) as error:
                    with self.assertRaisesRegex(RuntimeError,"RateLimitError"): worker_sdk.run("fixture")
                    self.assertEqual("RETICLE_RETRY_SAFE" in error.getvalue(),not mutate)

    def test_rag_snapshot_replaces_stale_sources(self):
        import rag_tools
        class Collection:
            def __init__(self): self.rows=[]
            def add(self, **kwargs): self.rows.extend(kwargs["metadatas"])
            def delete(self, where): self.rows=[row for row in self.rows if row["source"]!=where["source"]]
        class Client:
            def __init__(self): self.collections={}
            def create_collection(self,name,**kwargs):
                value=Collection();self.collections[name]=value;return value
            def get_collection(self,name,**kwargs): return self.collections[name]
            def delete_collection(self,name): del self.collections[name]
        client=Client()
        with tempfile.TemporaryDirectory() as temp,patch.object(rag_tools,"_client",return_value=client),patch.object(rag_tools,"_embedding",return_value=None):
            src=Path(temp)/"src";src.mkdir()
            (src/"a.tsx").write_text("a"*1800)
            (src/"b.py").write_text("b")
            rag_tools.index_directory(".",temp)
            first=rag_tools._collection(temp)
            self.assertEqual(len(first.rows),4)
            (src/"a.tsx").write_text("short")
            (src/"b.py").unlink()
            rag_tools.index_directory(".",temp)
            current=rag_tools._collection(temp)
            self.assertEqual(current.rows,[{"source":"a.tsx","offset":0}])
            rag_tools.remove_path_from_index("a.tsx",temp)
            self.assertEqual(current.rows,[])
            self.assertEqual(len(first.rows),4)

    def test_comfy_requires_local_configuration_and_safe_path(self):
        import comfy_tools
        with tempfile.TemporaryDirectory() as temp,patch.dict("os.environ",{},clear=True):
            with self.assertRaises(ValueError): comfy_tools.generate_local_asset("fixture","../escape",temp)
            with self.assertRaisesRegex(ValueError,"COMFYUI_CHECKPOINT"):
                comfy_tools.generate_local_asset("fixture","image.png",temp)
            with patch.dict("os.environ",{"COMFYUI_HOST":"https://example.com"}):
                with self.assertRaisesRegex(ValueError,"local ComfyUI"):
                    comfy_tools.generate_local_asset("fixture","image.png",temp)

if __name__ == "__main__":
    unittest.main()
