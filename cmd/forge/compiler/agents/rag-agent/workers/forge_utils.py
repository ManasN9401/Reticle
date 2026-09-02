import os
import json
import uuid
import hashlib

def emit_log(msg):
    print(f"[TOOL] {msg}")

try:
    import chromadb
    from chromadb.utils import embedding_functions
    DB_AVAILABLE = True
except ImportError:
    DB_AVAILABLE = False
    emit_log("WARNING: chromadb not found. RAG tools will fail.")

def get_chroma_client():
    db_path = r"d:\Reticle\runtime\vector_db"
    os.makedirs(db_path, exist_ok=True)
    return chromadb.PersistentClient(path=db_path)

def get_embedding_function():
    # Use all-MiniLM-L6-v2 via sentence-transformers (fast, local CPU/GPU)
    return embedding_functions.SentenceTransformerEmbeddingFunction(model_name="all-MiniLM-L6-v2")

def index_directory(path: str) -> str:
    emit_log(f"Executing index_directory: {path}")
    if not DB_AVAILABLE:
        return "Error: chromadb is not installed."
        
    if not os.path.exists(path):
        return f"Error: Path {path} does not exist."
        
    client = get_chroma_client()
    collection = client.get_or_create_collection(
        name="workspace_index", 
        embedding_function=get_embedding_function()
    )
    
    docs = []
    metadatas = []
    ids = []
    
    chunk_size = 1000
    overlap = 200
    
    indexed_files = 0
    for root, _, files in os.walk(path):
        # Skip common ignore dirs
        if any(ignored in root for ignored in ['.git', 'node_modules', '__pycache__', 'vector_db']):
            continue
            
        for file in files:
            file_path = os.path.join(root, file)
            # Skip non-text files quickly
            if not file_path.endswith(('.py', '.js', '.ts', '.md', '.go', '.yaml', '.yml', '.json', '.txt', '.html', '.css')):
                continue
                
            try:
                with open(file_path, "r", encoding="utf-8") as f:
                    content = f.read()
            except Exception:
                continue
                
            # Chunking logic
            start = 0
            while start < len(content):
                end = min(start + chunk_size, len(content))
                chunk = content[start:end]
                
                chunk_id = hashlib.md5(f"{file_path}_{start}".encode()).hexdigest()
                
                docs.append(chunk)
                metadatas.append({"source": file_path, "start_index": start})
                ids.append(chunk_id)
                
                if end == len(content):
                    break
                start += (chunk_size - overlap)
                
            indexed_files += 1
            
            # Batch insert every 100 docs to avoid memory explosion
            if len(docs) > 100:
                collection.upsert(documents=docs, metadatas=metadatas, ids=ids)
                docs = []
                metadatas = []
                ids = []
                
    # Insert remaining
    if len(docs) > 0:
        collection.upsert(documents=docs, metadatas=metadatas, ids=ids)
        
    return f"Successfully indexed {indexed_files} files into ChromaDB."

def query_knowledge(query: str, n_results: int = 5) -> str:
    emit_log(f"Executing query_knowledge for: '{query}'")
    if not DB_AVAILABLE:
        return "Error: chromadb is not installed."
        
    client = get_chroma_client()
    try:
        collection = client.get_collection(
            name="workspace_index",
            embedding_function=get_embedding_function()
        )
    except Exception:
        return "Error: Collection 'workspace_index' not found. Have you indexed a directory yet?"
        
    results = collection.query(
        query_texts=[query],
        n_results=n_results
    )
    
    if not results['documents'] or len(results['documents'][0]) == 0:
        return "No relevant documents found."
        
    formatted_results = []
    for i in range(len(results['documents'][0])):
        doc = results['documents'][0][i]
        meta = results['metadatas'][0][i]
        dist = results['distances'][0][i]
        
        formatted_results.append(
            f"--- Source: {meta.get('source')} (Distance: {dist:.4f}) ---\n{doc}\n"
        )
        
    return "\n".join(formatted_results)

def read_file(path: str) -> str:
    emit_log(f"Executing read_file: {path}")
    if not os.path.exists(path):
        return f"Error: {path} not found."
    with open(path, "r", encoding="utf-8") as f:
        return f.read()

def reset_knowledge_base() -> str:
    emit_log("Executing reset_knowledge_base")
    if not DB_AVAILABLE:
        return "Error: chromadb is not installed."
    client = get_chroma_client()
    try:
        client.delete_collection(name="workspace_index")
        return "Knowledge base successfully reset."
    except Exception as e:
        return f"Error resetting knowledge base: {str(e)}"

def remove_path_from_index(path: str) -> str:
    emit_log(f"Executing remove_path_from_index for {path}")
    if not DB_AVAILABLE:
        return "Error: chromadb is not installed."
    client = get_chroma_client()
    try:
        collection = client.get_collection(name="workspace_index")
        collection.delete(where={"source": {"$contains": path}})
        return f"Successfully removed chunks containing path '{path}' from the index."
    except Exception as e:
        return f"Error removing path: {str(e)}"

TOOLS_REGISTRY = [
    {
        "type": "function",
        "function": {
            "name": "index_directory",
            "description": "Walks a directory, chunks text/code files, and embeds them into a local ChromaDB.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "Absolute path to the directory to index."}
                },
                "required": ["path"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "query_knowledge",
            "description": "Perform a semantic vector search across previously indexed documents.",
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "The natural language query or concept to search for."},
                    "n_results": {"type": "integer", "description": "Number of chunks to return (default 5)."}
                },
                "required": ["query"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "read_file",
            "description": "Read content from a local file directly.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string"}
                },
                "required": ["path"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "reset_knowledge_base",
            "description": "Deletes the entire workspace_index collection to start fresh.",
            "parameters": {
                "type": "object",
                "properties": {},
                "required": []
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "remove_path_from_index",
            "description": "Removes all indexed chunks whose source path contains the provided string. Useful for removing stale directories or files.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "The path substring to match and delete."}
                },
                "required": ["path"]
            }
        }
    }
]

def execute_tool(name: str, args: dict) -> str:
    if name == "index_directory":
        return index_directory(args.get("path"))
    elif name == "query_knowledge":
        return query_knowledge(args.get("query"), args.get("n_results", 5))
    elif name == "read_file":
        return read_file(args.get("path"))
    elif name == "reset_knowledge_base":
        return reset_knowledge_base()
    elif name == "remove_path_from_index":
        return remove_path_from_index(args.get("path"))
    else:
        return f"Unknown tool: {name}"
