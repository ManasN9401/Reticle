import litellm
from dotenv import load_dotenv
load_dotenv(".env")
litellm.suppress_debug_info = True

models_to_test = [
    "gemini/gemini-2.5-flash",
    "gemini/gemini-3.5-flash",
    "gemini/gemini-3.5-flash-lite",
    "groq/qwen/qwen3.6-27b",
    "groq/groq/compound-mini"
]

for m in models_to_test:
    try:
        response = litellm.completion(
            model=m,
            messages=[{"role": "user", "content": "hi"}],
        )
        print(f"[SUCCESS] {m}")
    except Exception as e:
        print(f"[FAILED] {m} - {type(e).__name__}: {str(e)}")
