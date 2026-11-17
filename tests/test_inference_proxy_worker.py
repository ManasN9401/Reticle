"""RFC-052: given a proxy session, the worker sends its requests to Forge and uses no provider key."""
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import comfy_tools
import worker_sdk


class Done(Exception):
    pass


def first_request(environment, parameters):
    seen = {}

    def completion(**kwargs):
        seen.update(kwargs)
        raise Done()

    with tempfile.TemporaryDirectory() as temp:
        (Path(temp) / "src").mkdir()
        request = {"id": "execution|node", "execution": "execution", "memory": {"workspace_dir": temp},
                   "parameters": {"llm_model": "groq/openai/gpt-oss-120b", **parameters}}
        with patch.dict(sys.modules, {"litellm": SimpleNamespace(completion=completion)}), \
                patch.dict("os.environ", {"RETICLE_LOCAL_GPU_COORDINATION": "false", **environment}, clear=True), \
                patch.object(comfy_tools, "ollama_model_loaded", return_value=True), \
                patch.object(sys, "stdin", io.StringIO(json.dumps(request))), \
                patch.object(sys, "stdout", io.StringIO()), patch.object(sys, "stderr", io.StringIO()):
            try:
                worker_sdk.run("Fixture", kind="writing")
            except Done:
                pass
    return seen


class InferenceProxyWorker(unittest.TestCase):
    def test_a_proxied_worker_calls_forge_with_its_session_token(self):
        seen = first_request({"RETICLE_INFERENCE_URL": "http://127.0.0.1:5555/v1", "RETICLE_INFERENCE_TOKEN": "session-token"},
                             {"llm_api_base": "https://provider.example/v1"})
        self.assertEqual(seen["api_base"], "http://127.0.0.1:5555/v1")
        self.assertEqual(seen["api_key"], "session-token")
        self.assertTrue(seen["model"].startswith("openai/"), "the proxy speaks the OpenAI protocol")

    def test_without_a_session_the_direct_path_is_unchanged(self):
        seen = first_request({"GROQ_API_KEY": "direct-key"}, {"api_key": "GROQ_API_KEY"})
        self.assertEqual(seen["api_key"], "direct-key")
        self.assertEqual(seen["model"], "groq/openai/gpt-oss-120b")
        self.assertNotIn("api_base", seen)


if __name__ == "__main__":
    unittest.main()
