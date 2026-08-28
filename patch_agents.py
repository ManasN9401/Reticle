import os
import glob

target_dir = r"d:\HyperParallel\.hyperparallel\sessions\exec-001\agents"

search_str_1 = """                    messages=messages,
                    tools=tools,
                    parallel_tool_calls=False
                )"""

replace_str_1 = """                    messages=messages,
                    tools=tools,
                    parallel_tool_calls=False,
                    timeout=60
                )"""

search_str_2 = 'if "RateLimit" in err_str or "429" in err_str or "quota" in err_str.lower() or "overloaded" in err_str.lower() or "NotFoundError" in err_str or "404" in err_str or "APIError" in err_str or "APIConnectionError" in err_str or "502" in err_str or "503" in err_str or "too large" in err_str.lower() or "context_window" in err_str.lower() or "max_tokens" in err_str.lower() or "BadRequest" in err_str or "InvalidRequest" in err_str or "model_ter" in err_str.lower() or "invalid_request_error" in err_str.lower():'

replace_str_2 = 'if "RateLimit" in err_str or "429" in err_str or "quota" in err_str.lower() or "overloaded" in err_str.lower() or "NotFoundError" in err_str or "404" in err_str or "APIError" in err_str or "APIConnectionError" in err_str or "502" in err_str or "503" in err_str or "too large" in err_str.lower() or "context_window" in err_str.lower() or "max_tokens" in err_str.lower() or "BadRequest" in err_str or "InvalidRequest" in err_str or "model_ter" in err_str.lower() or "invalid_request_error" in err_str.lower() or "402" in err_str or "payment" in err_str.lower() or "credits" in err_str.lower() or "purchased" in err_str.lower() or "authenticationerror" in err_str.lower() or "timeout" in err_str.lower():'

count = 0
for root, _, files in os.walk(target_dir):
    for f in files:
        if f.endswith(".py"):
            path = os.path.join(root, f)
            with open(path, "r", encoding="utf-8") as file:
                content = file.read()
            
            if search_str_1 in content or search_str_2 in content:
                content = content.replace(search_str_1, replace_str_1)
                content = content.replace(search_str_2, replace_str_2)
                
                with open(path, "w", encoding="utf-8") as file:
                    file.write(content)
                count += 1
                
print(f"Patched {count} files.")
