import os
import sys
from litellm import completion

# litellm.set_verbose = True

def main():
    model = "groq/qwen-2.5-32b" # Wait, what exactly is the qwen model name on Groq?
    # Let's try the one the router used:
    model = "groq/qwen/qwen3.6-27b" # Wait, if Groq doesn't host this, it will fail.
    # Let's fetch models first if it fails.

    try:
        print(f"Testing model: {model}")
        api_key = os.environ.get("GROQ_API_KEY_2")
        if not api_key:
            print("No GROQ_API_KEY_2 found.")
            return

        resp = completion(
            model=model,
            api_key=api_key,
            max_tokens=4000,
            messages=[{"role": "user", "content": "Respond with the word Hello."}]
        )
        print("Success!")
        print(resp.choices[0].message.content)
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    main()
