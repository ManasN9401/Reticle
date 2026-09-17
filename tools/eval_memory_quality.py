"""Evaluate whether a model uses selected Reticle memory amid distractors.

Offline mode validates the fixture and prompt contract without contacting a model.
Pass --model to run paired no-memory, full-context, and selected-memory trials.
"""
import argparse
import json
from pathlib import Path
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "cmd" / "forge" / "compiler" / "lib"))
import worker_sdk

def normalize(value):
    return " ".join(str(value).strip().lower().split()).strip("`'\". ")

def load_cases(path):
    cases = json.loads(path.read_text(encoding="utf-8"))
    ids = set()
    for case in cases:
        required = {"id", "question", "selected_memory", "distractor_memory", "expected"}
        if set(case) != required or case["id"] in ids:
            raise ValueError(f"invalid or duplicate case: {case.get('id')}")
        ids.add(case["id"])
        selected = json.dumps(case["selected_memory"], ensure_ascii=False)
        if normalize(case["expected"]) not in normalize(selected):
            raise ValueError(f"expected answer is absent from selected memory: {case['id']}")
    return cases

def context_for(case, mode):
    memory = {"workspace_dir": "excluded-control"}
    if mode == "selected": memory.update(case["selected_memory"])
    elif mode == "full": memory.update(case["distractor_memory"]); memory.update(case["selected_memory"])
    req = {"parameters": {"user_prompt": case["question"]}, "inputs": []}
    return worker_sdk.build_user_context(req, memory)

def run_model(model, context):
    from litellm import completion
    response = completion(
        model=model,
        messages=[
            {"role": "system", "content": "Answer from the supplied JSON only. If the answer is absent, reply UNKNOWN. Follow the requested output format."},
            {"role": "user", "content": json.dumps(context, ensure_ascii=False)},
        ],
        temperature=0,
        max_tokens=32,
        timeout=60,
        num_retries=0,
    )
    usage = getattr(response, "usage", None)
    if hasattr(usage, "model_dump"):
        usage = usage.model_dump()
    elif usage is not None and not isinstance(usage, dict):
        usage = {name: getattr(usage, name) for name in ("prompt_tokens", "completion_tokens", "total_tokens") if hasattr(usage, name)}
    return response.choices[0].message.content, (usage or {})

def evaluate(cases, model=None, attempts=1):
    modes = ("none", "full", "selected")
    results = {"model": model, "attempts": attempts, "cases": [], "summary": {}}
    if model is None:
        prompt_bytes = {mode: 0 for mode in modes}
        for case in cases:
            selected = context_for(case, "selected")["shared_memory"]
            full = context_for(case, "full")["shared_memory"]
            none = context_for(case, "none")["shared_memory"]
            sizes = {mode: len(json.dumps(context_for(case, mode), ensure_ascii=False).encode("utf-8")) for mode in modes}
            for mode in modes: prompt_bytes[mode] += sizes[mode]
            results["cases"].append({"id":case["id"], "contract_pass": selected==case["selected_memory"] and not none and len(full)>=len(selected), "prompt_bytes": sizes})
        results["summary"] = {"mode":"offline-contract", "passed":sum(x["contract_pass"] for x in results["cases"]), "total":len(cases)}
        results["prompt_metrics"] = {"bytes_by_mode": prompt_bytes}
        return results
    totals = {mode:0 for mode in modes}
    per_case = {mode:[] for mode in modes}
    for case in cases:
        row={"id":case["id"],"expected":case["expected"],"trials":[]}
        for attempt in range(attempts):
            for mode in modes:
                error=None
                context = context_for(case,mode)
                trial_started = time.time()
                try:
                    model_result=run_model(model,context)
                    if isinstance(model_result, tuple):
                        answer, usage=model_result
                    else:
                        answer, usage=model_result, {}
                except Exception as exc:
                    answer=""
                    usage={}
                    error=type(exc).__name__
                passed=error is None and normalize(answer)==normalize(case["expected"])
                totals[mode]+=int(passed)
                trial={"attempt":attempt+1,"mode":mode,"answer":answer,"pass":passed,
                       "latency_seconds":round(time.time()-trial_started,3),
                       "prompt_bytes":len(json.dumps(context,ensure_ascii=False).encode("utf-8")),
                       "usage":usage}
                if error is not None: trial["error"]=error
                row["trials"].append(trial)
        results["cases"].append(row)
        for mode in modes:
            per_case[mode].append([trial["pass"] for trial in row["trials"] if trial["mode"]==mode])
    denominator=len(cases)*attempts
    results["summary"]={mode:{"passed":totals[mode],"total":denominator,"accuracy":totals[mode]/denominator,
        "pass_at_k":sum(any(trials) for trials in per_case[mode])/len(cases),
        "pass_power_k":sum(all(trials) for trials in per_case[mode])/len(cases)} for mode in modes}
    results["summary"]["selected_uplift_vs_none"] = results["summary"]["selected"]["accuracy"]-results["summary"]["none"]["accuracy"]
    results["summary"]["selected_uplift_vs_full"] = results["summary"]["selected"]["accuracy"]-results["summary"]["full"]["accuracy"]
    return results

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument("--cases",type=Path,default=ROOT/"evals"/"memory-quality"/"cases.json")
    parser.add_argument("--model",help="LiteLLM model ID; omitted for offline contract validation")
    parser.add_argument("--attempts",type=int,default=1)
    parser.add_argument("--output",type=Path)
    args=parser.parse_args()
    if args.attempts < 1 or args.attempts > 10: parser.error("--attempts must be 1..10")
    started=time.time(); result=evaluate(load_cases(args.cases),args.model,args.attempts); result["elapsed_seconds"]=round(time.time()-started,3)
    rendered=json.dumps(result,indent=2,ensure_ascii=False)
    if args.output:
        args.output.parent.mkdir(parents=True,exist_ok=True)
        args.output.write_text(rendered+"\n",encoding="utf-8")
    print(rendered)

if __name__ == "__main__": main()
