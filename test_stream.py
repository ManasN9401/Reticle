import litellm
import sys

litellm.set_verbose = True

messages = [{"role": "user", "content": "What is 2+2? Use the calculator tool."}]
tools = [{
    "type": "function",
    "function": {
        "name": "calculator",
        "description": "Calculates math expressions",
        "parameters": {
            "type": "object",
            "properties": {"expr": {"type": "string"}},
            "required": ["expr"]
        }
    }
}]

response = litellm.completion(
    model="openai/C:\\Users\\nathm\\models\\Qwen3.6-35B-A3B\\Qwen_Qwen3.6-35B-A3B-IQ2_M.gguf",
    api_base="http://127.0.0.1:8080/v1",
    api_key="sk-no-key",
    messages=messages,
    tools=tools,
    stream=True
)

for chunk in response:
    print(chunk)
