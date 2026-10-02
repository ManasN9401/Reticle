from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import worker_sdk

PROPERTIES = {
    "prompt": "string",
    "output_path": "string",
    "checkpoint": {"type": "string", "optional": True},
    "width": {"type": "integer", "optional": True},
}


class ToolArguments(unittest.TestCase):
    def error(self, **args):
        return worker_sdk._tool_argument_error("tool", PROPERTIES, args)

    def test_optional_arguments_may_be_omitted(self):
        self.assertIsNone(self.error(prompt="a", output_path="b.png"))
        self.assertIsNone(self.error(prompt="a", output_path="b.png", checkpoint="c", width=512))

    def test_required_unknown_and_mistyped_arguments_are_named(self):
        self.assertIn("missing 'output_path'", self.error(prompt="a"))
        self.assertIn("unknown argument 'extra'", self.error(prompt="a", output_path="b", extra="x"))
        self.assertIn("'width' must be integer", self.error(prompt="a", output_path="b", width="512"))
        self.assertIn("'prompt' must be string", self.error(prompt=3, output_path="b"))

    def test_bool_is_not_an_integer(self):
        self.assertIn("'width' must be integer", self.error(prompt="a", output_path="b", width=True))

    def test_error_lists_required_and_optional_arguments(self):
        message = self.error(prompt="a")
        self.assertIn("Required: output_path (string), prompt (string)", message)
        self.assertIn("Optional: checkpoint (string), width (integer)", message)

    def test_plain_string_tools_keep_their_strict_contract(self):
        properties = {"path": "string"}
        self.assertIsNone(worker_sdk._tool_argument_error("read_file", properties, {"path": "a"}))
        self.assertIsNotNone(worker_sdk._tool_argument_error("read_file", properties, {}))
        self.assertIsNotNone(worker_sdk._tool_argument_error("read_file", properties, {"path": 1}))

    def test_descriptor_requires_only_non_optional_arguments(self):
        schema = worker_sdk._local_tool_descriptor("tool", "d", PROPERTIES, None)["schema"]
        self.assertEqual(schema["required"], ["prompt", "output_path"])
        self.assertEqual(schema["properties"]["width"], {"type": "integer"})
        self.assertFalse(schema["additionalProperties"])

    def test_effectful_descriptors_use_the_shared_list(self):
        self.assertEqual(worker_sdk._local_tool_descriptor("generate_local_asset", "d", {}, None)["effect"], "effect_started")
        self.assertEqual(worker_sdk._local_tool_descriptor("read_file", "d", {}, None)["effect"], "no_effect")



class IterationLimit(unittest.TestCase):
    def test_default_depends_on_image_capability(self):
        self.assertEqual(worker_sdk._iteration_limit({}, {"fs.write"}), 30)
        self.assertEqual(worker_sdk._iteration_limit({}, {"image.local"}), 60)

    def test_memory_value_is_honoured_and_clamped(self):
        self.assertEqual(worker_sdk._iteration_limit({"llm_max_iterations": 45}, set()), 45)
        self.assertEqual(worker_sdk._iteration_limit({"llm_max_iterations": 0}, set()), 1)
        self.assertEqual(worker_sdk._iteration_limit({"llm_max_iterations": 5000}, set()), 200)

    def test_invalid_value_falls_back_to_the_default(self):
        self.assertEqual(worker_sdk._iteration_limit({"llm_max_iterations": "many"}, set()), 30)
        self.assertEqual(worker_sdk._iteration_limit({"llm_max_iterations": None}, {"image.local"}), 60)


if __name__ == "__main__":
    unittest.main()
