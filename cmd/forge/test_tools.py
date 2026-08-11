import sys
import os

with open("compiler/workers/coder.py", "r", encoding="utf-8") as f:
    content = f.read()

# The utils_code block starts with `utils_code = """` and ends with `"""`
start_marker = 'utils_code = """'
end_marker = '"""'

start_idx = content.find(start_marker)
if start_idx == -1:
    print("Could not find utils_code start")
    sys.exit(1)

start_idx += len(start_marker)
end_idx = content.find(end_marker, start_idx)

utils_code = content[start_idx:end_idx]

# Execute the utils_code to bring the functions into the current namespace
exec(utils_code)

# Setup workspace
test_workspace = os.path.join(os.getcwd(), "test_workspace")
os.makedirs(os.path.join(test_workspace, "src"), exist_ok=True)

print("=== STARTING TESTS ===")

print("\n--- TEST: write_file ---")
print("Input: path='test.py', content='print(\"Hello World\")\\n'")
print("Output:", write_file("test.py", "print('Hello World')\n", test_workspace))

print("\n--- TEST: read_file ---")
print("Input: path='test.py'")
print("Output:", read_file("test.py", test_workspace))

print("\n--- TEST: list_dir ---")
print("Input: path='.'")
print("Output:", list_dir(".", test_workspace))

print("\n--- TEST: replace_file_content ---")
print("Input: path='test.py', target_content='Hello World', replacement_content='Universe'")
print("Output:", replace_file_content("test.py", "Hello World", "Universe", test_workspace))

print("\n--- TEST: search_codebase ---")
print("Input: regex_pattern='Universe'")
print("Output:", search_codebase("Universe", test_workspace))

print("\n--- TEST: execute_terminal_command ---")
print("Input: command='cat test.py'")
print("Output:", execute_terminal_command("cat test.py", test_workspace))

print("\n--- TEST: read_url ---")
print("Input: url='https://example.com'")
print("Output:", read_url("https://example.com", test_workspace)[:200] + "...")

print("\n=== TESTS COMPLETE ===")
