import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location("memory_quality",ROOT/"tools"/"eval_memory_quality.py")
memory_quality=importlib.util.module_from_spec(spec);spec.loader.exec_module(memory_quality)

class MemoryQualityHarnessTests(unittest.TestCase):
    def test_offline_contract_eval(self):
        cases=memory_quality.load_cases(ROOT/"evals"/"memory-quality"/"cases.json")
        result=memory_quality.evaluate(cases)
        self.assertEqual(result["summary"],{"mode":"offline-contract","passed":4,"total":4})

    def test_modes_and_exact_grading(self):
        case=memory_quality.load_cases(ROOT/"evals"/"memory-quality"/"cases.json")[1]
        self.assertEqual(memory_quality.context_for(case,"none")["shared_memory"],{})
        self.assertEqual(memory_quality.context_for(case,"selected")["shared_memory"],{"deployment_region":"eu-west-2"})
        self.assertIn("stale_global_region",memory_quality.context_for(case,"full")["shared_memory"])
        self.assertEqual(memory_quality.normalize(" `EU-WEST-2`. "),"eu-west-2")

    def test_paired_model_metrics(self):
        case=memory_quality.load_cases(ROOT/"evals"/"memory-quality"/"cases.json")[:1]
        def answer(_model,context):
            return context["shared_memory"].get("dataset_revision","UNKNOWN")
        with patch.object(memory_quality,"run_model",side_effect=answer):
            summary=memory_quality.evaluate(case,"fixture/model",2)["summary"]
        self.assertEqual(summary["selected"]["accuracy"],1)
        self.assertEqual(summary["none"]["accuracy"],0)
        self.assertEqual(summary["selected"]["pass_power_k"],1)
        self.assertEqual(summary["selected_uplift_vs_none"],1)

if __name__ == "__main__": unittest.main()
