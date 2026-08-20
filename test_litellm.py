import litellm
from dotenv import load_dotenv
load_dotenv(".env")
litellm.suppress_debug_info = True

models_to_test = [
    "gemini/gemini-1.5-flash-latest",
    "gemini/gemini-pro",
    "gemini/gemini-1.5-pro",
    "gemini/gemini-1.5-pro-latest",
    "gemini-1.5-flash",
    "gemini-1.5-pro"
]

for m in models_to_test:
    try:
        response = litellm.completion(
            model=m,
            messages=[{"role": "user", "content": "hi"}],
        )
        print(f"[SUCCESS] {m}")
    except Exception as e:
        print(f"[FAILED] {m} - {str(e)}")
