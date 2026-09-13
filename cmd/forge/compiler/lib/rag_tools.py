"""Workspace-scoped, versioned vector indexes with exact source reconciliation."""
from functools import lru_cache
import hashlib
import json
import os
os.environ["HF_HUB_DISABLE_SYMLINKS_WARNING"] = "1"
import warnings
warnings.filterwarnings("ignore", message=".*unauthenticated requests.*")
from pathlib import Path
import uuid
from forge_utils import safe_path, MAX_FILE

def _root(workspace):
    root=safe_path(workspace,".rag",base="")
    root.mkdir(exist_ok=True)
    return root

@lru_cache(maxsize=8)
def _client(root):
    import chromadb
    return chromadb.PersistentClient(path=root)

@lru_cache(maxsize=2)
def _embedding(model):
    from chromadb.utils.embedding_functions import SentenceTransformerEmbeddingFunction
    return SentenceTransformerEmbeddingFunction(model_name=model)

def _collection(workspace):
    root=_root(workspace)
    manifest=json.loads((root/"current.json").read_text())
    return _client(str(root)).get_collection(manifest["collection"],embedding_function=_embedding(manifest["embedding_model"]))

def index_directory(path, workspace_dir):
    root=_root(workspace_dir)
    source=safe_path(workspace_dir,path)
    model=os.getenv("RETICLE_EMBEDDING_MODEL","sentence-transformers/all-MiniLM-L6-v2")
    client=_client(str(root));name="index-"+uuid.uuid4().hex
    collection=client.create_collection(name,embedding_function=_embedding(model))
    count=0
    chunks=0
    try:
        for file in source.rglob("*"):
            if not file.is_file() or any(p in {".git",".reticle","node_modules","__pycache__",".venv"} for p in file.parts):
                continue
            rel=file.relative_to(Path(workspace_dir).resolve()/"src").as_posix()
            file=safe_path(workspace_dir,rel)
            if file.stat().st_size>MAX_FILE or file.suffix not in {".py",".js",".ts",".tsx",".jsx",".md",".go",".yaml",".yml",".json",".txt",".html",".css"}:
                continue
            try:text=file.read_text(encoding="utf-8")
            except UnicodeError:continue
            for offset in range(0,len(text),800):
                chunks += 1
                if chunks > 20000: raise ValueError("Index exceeds 20,000 chunks; select a smaller directory")
                chunk=text[offset:offset+1000]
                collection.add(ids=[hashlib.sha256((rel+":"+str(offset)).encode()).hexdigest()],documents=[chunk],metadatas=[{"source":rel,"offset":offset}])
            count+=1
        temporary=root/(name+".json")
        temporary.write_text(json.dumps({"collection":name,"embedding_model":model,"files":count}))
        os.replace(temporary,root/"current.json")
    except Exception:
        client.delete_collection(name)
        raise
    return f"Indexed {count} files. Queries now use the complete new snapshot; removed files and trailing chunks are excluded."

def query_knowledge(query, workspace_dir):
    collection=_collection(workspace_dir)
    if collection.count()==0:return "Index is empty"
    result=collection.query(query_texts=[query],n_results=min(5,collection.count()))
    return json.dumps(result)[:20000]

def remove_path_from_index(path, workspace_dir):
    rel=safe_path(workspace_dir,path).relative_to(Path(workspace_dir).resolve()/"src").as_posix()
    _collection(workspace_dir).delete(where={"source":rel})
    return "Removed exact source from current workspace index"
