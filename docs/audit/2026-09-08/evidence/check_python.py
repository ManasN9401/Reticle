"""Offline audit probes. Uses only synthetic inputs and temporary fixtures.

Run from repository root with an existing Python interpreter. No model calls,
cloud requests, package installs, real credentials, or user files are used.
"""
import ast
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[4]
tracked = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).decode().split('\0')
tracked = [p for p in tracked if p]
results = {'tracked_files': len(tracked), 'python_syntax': [], 'generators': [], 'fixtures': {}}
for name in tracked:
    if name.endswith('.py'):
        try:
            ast.parse((ROOT / name).read_text(encoding='utf-8-sig'), filename=name)
        except (SyntaxError, UnicodeError) as exc:
            results['python_syntax'].append({'path': name, 'error': str(exc)})
results['python_files_checked'] = sum(p.endswith('.py') for p in tracked)
results['empty_files'] = [p for p in tracked if (ROOT / p).is_file() and (ROOT / p).stat().st_size == 0]
results['go_tests'] = [p for p in tracked if p.endswith('_test.go')]
results['ci_workflows'] = [p for p in tracked if p.startswith('.github/workflows/') and p.endswith(('.yaml', '.yml'))]

with tempfile.TemporaryDirectory(prefix='reticle-audit-') as temp:
    base = Path(temp)
    for agent, filename in [('coder-agent','coder.py'), ('hermes-coder-agent','hermes.py'), ('quant-agent','quant.py'), ('osint-agent','osint.py'), ('browser-agent','browser.py'), ('scaffolder-agent','scaffolder.py')]:
        workspace = base / agent
        workspace.mkdir()
        dag = {'workflow_name':'Audit fixture', 'agents':[{'id':'audit-worker', 'name':'Audit Worker', 'description':'Synthetic offline fixture', 'is_new':True, 'system_prompt':'Return a synthetic result.', 'inputs':[], 'memory':[], 'skills':[]}], 'nodes':[{'id':'audit-node','agent_id':'audit-worker','modality':'coding','parameters':{'effort':'high'}}], 'edges':[]}
        req = {'id':'compile-exec-audit|generate','execution':'compile-exec-audit','memory':{'workspace_dir':str(workspace)}, 'inputs':[{'name':'DAG JSON','data':json.dumps(dag)}]}
        script = ROOT / 'cmd/forge/compiler/agents' / agent / 'workers' / filename
        proc = subprocess.run([sys.executable,'-B',str(script)],input=json.dumps(req)+'\n',encoding='utf-8',capture_output=True,timeout=10, env={**os.environ,'PYTHONIOENCODING':'utf-8'})
        item = {'agent':agent,'exit':proc.returncode,'stderr':proc.stderr[-1800:], 'generated_syntax':[]}
        for generated in workspace.rglob('*.py'):
            try:
                ast.parse(generated.read_text(encoding='utf-8'))
            except SyntaxError as exc:
                item['generated_syntax'].append(str(exc))
        if agent == 'scaffolder-agent':
            item['workflow'] = next(workspace.rglob('*.yaml')).read_text(encoding='utf-8')
            item['workflow'] = (workspace / 'workflows/workflow_exec-audit.yaml').read_text(encoding='utf-8')
        results['generators'].append(item)

    utils = ROOT / 'cmd/forge/compiler/agents/ml-agent/workers/forge_utils.py'
    spec = importlib.util.spec_from_file_location('audit_utils', utils)
    module = importlib.util.module_from_spec(spec)
    sys.dont_write_bytecode = True
    spec.loader.exec_module(module)
    workspace = base / 'containment'
    (workspace / 'src').mkdir(parents=True)
    results['fixtures']['traversal_write_result'] = module.write_file('../outside-src.txt','synthetic',str(workspace),{})
    results['fixtures']['traversal_file_created'] = (workspace / 'outside-src.txt').exists()
    results['fixtures']['absolute_write_result'] = module.write_file(str(base / 'absolute.txt'),'synthetic',str(workspace),{})
    results['fixtures']['absolute_file_created'] = (base / 'absolute.txt').exists()
    original = 'value = "\\n"\n'
    module.write_file('literal.py',original,str(workspace),{})
    written = (workspace / 'src/literal.py').read_text(encoding='utf-8')
    results['fixtures']['literal_preserved'] = original == written
    try:
        ast.parse(written)
        results['fixtures']['literal_syntax_valid'] = True
    except SyntaxError:
        results['fixtures']['literal_syntax_valid'] = False

    # Replace only subprocess.run with a recorder: do not start Docker or a shell.
    calls = []
    def record(command, **kwargs):
        calls.append({'command':command, 'cwd':kwargs.get('cwd')})
        return subprocess.CompletedProcess(command,0,stdout='',stderr='')
    actual_run = module.subprocess.run
    module.subprocess.run = record
    try:
        module.execute_terminal_command('python train.py',str(workspace),True)
        module.execute_terminal_command('python train.py',str(workspace),False)
    finally:
        module.subprocess.run = actual_run
    results['fixtures']['terminal_calls'] = calls
    # No payload argument: exercises only the immediately failing stdin contract.
    hitl = ROOT / 'cmd/forge/compiler/agents/hitl-agent/workers/hitl.py'
    p = subprocess.run([sys.executable,'-B',str(hitl)],input='{"id":"audit"}\n',capture_output=True,text=True,timeout=5)
    results['fixtures']['hitl_stdin'] = {'exit':p.returncode,'stdout':p.stdout.strip()}

output = json.dumps(results,indent=2)
Path(__file__).with_name('python-results.json').write_text(output + '\n', encoding='utf-8')
print(output)
