import os

base_dir = r'd:\HyperParallel\cmd\forge\compiler\agents'
coder_path = os.path.join(base_dir, 'coder-agent', 'workers', 'coder.py')
osint_dir = os.path.join(base_dir, 'osint-agent', 'workers')
os.makedirs(osint_dir, exist_ok=True)

with open(coder_path, 'r', encoding='utf-8') as f:
    content = f.read()

# Add google search tool to utils_code
search_tool_code = """
def google_search_and_scrape(query, workspace_dir):
    try:
        import requests
        from bs4 import BeautifulSoup
        import concurrent.futures
        import re
        
        num_results = 2
        url = 'https://www.google.com/search'
        params = {'q': query, 'num': num_results}
        headers = {'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/94.0.4606.61 Safari/537.3'}
        
        response = requests.get(url, params=params, headers=headers)
        soup = BeautifulSoup(response.text, 'html.parser')
        urls = [result.find('a')['href'] for result in soup.find_all('div', class_='tF2Cxc')]
        
        with concurrent.futures.ThreadPoolExecutor(max_workers=5) as executor:
            futures = [executor.submit(lambda u: (u, requests.get(u, headers=headers).text if isinstance(u, str) else None), u) for u in urls[:num_results] if isinstance(u, str)]
            results = []
            for future in concurrent.futures.as_completed(futures):
                u, html = future.result()
                soup = BeautifulSoup(html, 'html.parser')
                paragraphs = [p.text.strip() for p in soup.find_all('p') if p.text.strip()]
                text_content = ' '.join(paragraphs)
                text_content = re.sub(r'\\s+', ' ', text_content)
                if text_content:
                    results.append({'url': u, 'content': text_content[:5000]})
        import json
        return json.dumps(results)
    except Exception as e:
        return f"Error scraping: {e}"
"""

search_tool_schema = """    {
        "type": "function",
        "function": {
            "name": "google_search_and_scrape",
            "description": "Performs a Google search for the given query, retrieves the top search result URLs, and scrapes the text content from those pages in parallel using BeautifulSoup.",
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "The search query"}
                },
                "required": ["query"]
            }
        }
    },"""

# Insert the code and schema
content = content.replace('def read_url(url, workspace_dir):', search_tool_code + '\ndef read_url(url, workspace_dir):')
content = content.replace('tools = [', 'tools = [\n' + search_tool_schema)
content = content.replace('elif func_name == "read_file":', 'elif func_name == "google_search_and_scrape":\n                            res = google_search_and_scrape(args.get("query"), workspace_dir)\n                        elif func_name == "read_file":')
content = content.replace('from forge_utils import execute_terminal_command, read_file', 'from forge_utils import execute_terminal_command, google_search_and_scrape, read_file')

sys_prompt_old = '''sys_prompt += """\n\nYou are an autonomous agent equipped with tools. You must use the tools to read the workspace, execute tests, and modify files.
You have full root access to a Debian terminal via `execute_terminal_command`. You can test your work by running standard compilation or execution commands for your assigned language (e.g. `node src/index.js`, `python src/main.py`, `go build`). Use `read_url` to look up documentation if you are stuck. Use `list_dir` to explore the workspace instead of guessing file paths.'''

sys_prompt_new = '''sys_prompt += """\n\nYou are an OSINT (Open Source Intelligence) Agent equipped with advanced web search and scraping tools.
You must use your `google_search_and_scrape` tool to perform internet research and synthesize massive data sources for the user's requirements.
You also have full root access to a Debian terminal via `execute_terminal_command`. You can install libraries like playwright (`uv pip install playwright beautifulsoup4 requests && playwright install`) if you need to write custom scraping scripts!'''

content = content.replace(sys_prompt_old, sys_prompt_new)

with open(os.path.join(osint_dir, 'osint.py'), 'w', encoding='utf-8') as f:
    f.write(content)

print("Created osint.py successfully")
