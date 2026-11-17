"""Read-only views of runtime-owned attempt baselines; no user Git metadata access."""
import difflib
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

import forge_utils


def _ignored(path):
    excluded={".git",".reticle",".aws",".ssh",".venv","venv","node_modules","__pycache__",".reticle-go-cache",".reticle-npm-cache",".reticle-uv-cache"}
    return any(p.lower().rstrip(" .") in excluded or p.lower().startswith(".env") or p.lower().endswith(".reticle-lock") for p in Path(path).parts)


def _git(workspace, arguments, budget=16*1024*1024):
    repository=forge_utils.safe_path(workspace,"history/repository",base="")
    env={k:v for k,v in os.environ.items() if not k.upper().startswith("GIT_")}
    env.update(GIT_CONFIG_NOSYSTEM="1",GIT_CONFIG_GLOBAL=os.devnull,GIT_TERMINAL_PROMPT="0")
    command=["git","--no-pager","--git-dir="+str(repository),"-c","core.hooksPath="+str(repository.parent/"disabled-hooks"),*arguments]
    with tempfile.TemporaryFile() as output, tempfile.TemporaryFile() as errors:
        result=subprocess.run(command,stdout=output,stderr=errors,env=env,timeout=15)
        output.seek(0);raw=output.read(budget+1)
        if result.returncode or len(raw)>budget:
            raise ValueError("Checkpoint Git inspection failed or exceeds the output budget")
    return raw


def _baseline(workspace,checkpoint_id,task_id):
    if not re.fullmatch(r"[a-f0-9]{32}",checkpoint_id or ""):
        raise ValueError("Runtime checkpoint is unavailable; enable RETICLE_WORKSPACE_CHECKPOINTS before starting the attempt")
    target=forge_utils.safe_path(workspace,"history/records/"+checkpoint_id+".json",base="")
    with target.open("rb") as stream: raw=stream.read(8193)
    if len(raw)>8192: raise ValueError("Checkpoint record exceeds its budget")
    record=json.loads(raw)
    if record.get("id")!=checkpoint_id or record.get("task_id")!=task_id:
        raise PermissionError("Checkpoint does not belong to this task")
    sha=record.get("before","")
    if not re.fullmatch(r"[a-f0-9]{40}",sha): raise ValueError("Invalid checkpoint baseline")
    raw=_git(workspace,["ls-tree","-r","-z",sha],1024*1024)
    tree={}
    for entry in raw.split(b"\0"):
        if not entry: continue
        header,path=entry.split(b"\t",1)
        mode,kind,oid=header.decode("ascii").split()
        path=path.decode("utf-8")
        if kind!="blob" or mode not in {"100644","100755"} or not re.fullmatch(r"[a-f0-9]{40}",oid) or _ignored(path):
            raise ValueError("Unsupported checkpoint tree entry")
        forge_utils.safe_path(workspace,path,literal=True)
        tree[path]=(mode,oid)
    return record,tree


def _current(workspace):
    root=forge_utils.safe_path(workspace,".")
    current={};total=0
    for directory,dirs,names in os.walk(root,followlinks=False):
        dirs[:]=sorted(name for name in dirs if not _ignored(str((Path(directory)/name).relative_to(root))))
        for name in dirs:
            forge_utils.safe_path(workspace,(Path(directory)/name).relative_to(root).as_posix(),literal=True)
        for name in sorted(names):
            relative=(Path(directory)/name).relative_to(root).as_posix()
            if _ignored(relative): continue
            target=forge_utils.safe_path(workspace,relative,literal=True)
            if not target.is_file(): raise ValueError("Git inspection requires regular workspace files")
            with target.open("rb") as stream: raw=stream.read(16*1024*1024+1)
            total+=len(raw)
            if len(raw)>16*1024*1024 or total>64*1024*1024 or len(current)>=1000:
                raise ValueError("Git inspection exceeds the checkpoint file/byte budget")
            mode="100755" if os.name!="nt" and target.stat().st_mode&0o111 else "100644"
            oid=hashlib.sha1(b"blob "+str(len(raw)).encode()+b"\0"+raw).hexdigest()
            current[relative]=(mode,oid)
    return current


def _changes(before,after):
    return [{"path":path,"status":"A" if path not in before else "D" if path not in after else "M"}
            for path in sorted(set(before)|set(after)) if before.get(path)!=after.get(path)]


def git_status(workspace_dir,*,checkpoint_id,task_id,offset=1,limit=100):
    if isinstance(offset,bool) or not isinstance(offset,int) or offset<1 or isinstance(limit,bool) or not isinstance(limit,int) or not 1<=limit<=100:
        raise ValueError("offset must be positive and limit must be 1-100")
    record,tree=_baseline(workspace_dir,checkpoint_id,task_id)
    changes=_changes(tree,_current(workspace_dir))
    if offset>max(1,len(changes)): raise ValueError("offset is beyond the change list")
    selected=changes[offset-1:offset-1+limit]
    result={"checkpoint":record["id"],"before":record["before"],"total":len(changes),"changes":selected}
    while len(json.dumps(result,ensure_ascii=False))>max(1000,forge_utils.TOOL_RESULT_CHARS-400) and selected:
        selected.pop()
    result["next_offset"]=offset+len(selected) if offset-1+len(selected)<len(changes) else None
    return json.dumps(result,ensure_ascii=False)


def git_diff(workspace_dir,path=None,*,checkpoint_id,task_id,offset=1,limit=200):
    record,before=_baseline(workspace_dir,checkpoint_id,task_id)
    changes=_changes(before,_current(workspace_dir))
    if path is not None:
        path=forge_utils.canonical_workspace_path(path)
        forge_utils.safe_path(workspace_dir,path,literal=True)
        changes=[change for change in changes if change["path"]==path]
    parts=[f"Attempt baseline {record['id']} ({record['before']}). Changes are workspace observations, not proof of which actor made them.\n"]
    for change in changes[:20]:
        name=change["path"]
        old=b"";new=b""
        if name in before:
            size=int(_git(workspace_dir,["cat-file","-s",before[name][1]],100))
            if size>1024*1024:
                parts.append(f"{name}: original exceeds text diff budget; inspect checkpoint objects.\n");continue
            old=_git(workspace_dir,["cat-file","blob",before[name][1]],1024*1024)
        target=forge_utils.safe_path(workspace_dir,name,literal=True)
        if target.is_file():
            with target.open("rb") as stream: new=stream.read(1024*1024+1)
        try:
            if b"\0" in old+new or len(new)>1024*1024: raise UnicodeError()
            old_text,new_text=old.decode("utf-8"),new.decode("utf-8")
        except UnicodeError:
            parts.append(f"{name}: binary or large file changed ({change['status']}).\n");continue
        diff="".join(line if line.endswith("\n") else line+"\n\\ No newline at end of file\n" for line in difflib.unified_diff(old_text.splitlines(keepends=True),new_text.splitlines(keepends=True),fromfile="a/"+name,tofile="b/"+name))
        parts.append(diff or f"{name}: mode changed.\n")
        if sum(len(part) for part in parts)>forge_utils.MAX_FILE: break
    if len(changes)>20: parts.append("Only the first 20 changed files are shown; pass path for another file.\n")
    text="".join(parts)
    if len(text)>forge_utils.MAX_FILE: text=text[:forge_utils.MAX_FILE]+"\n[Diff capped at 1 MiB; narrow path.]"
    return forge_utils._page_of(text,offset,limit)
