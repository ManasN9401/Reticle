import os

base_dir = r'd:\Reticle\cmd\forge\compiler\agents'
coder_path = os.path.join(base_dir, 'coder-agent', 'workers', 'coder.py')
quant_dir = os.path.join(base_dir, 'quant-agent', 'workers')
os.makedirs(quant_dir, exist_ok=True)

with open(coder_path, 'r', encoding='utf-8') as f:
    content = f.read()

quant_tools_code = """
def get_current_stock_price(symbol, workspace_dir):
    try:
        import yfinance as yf
        stock = yf.Ticker(symbol)
        current_price = stock.info.get("regularMarketPrice", stock.info.get("currentPrice"))
        return str(current_price)
    except Exception as e:
        return f"Error: {e}"

def get_stock_fundamentals(symbol, workspace_dir):
    try:
        import yfinance as yf
        import json
        stock = yf.Ticker(symbol)
        info = stock.info
        fundamentals = {
            'symbol': symbol,
            'company_name': info.get('longName', ''),
            'sector': info.get('sector', ''),
            'industry': info.get('industry', ''),
            'market_cap': info.get('marketCap', None),
            'pe_ratio': info.get('forwardPE', None),
            'pb_ratio': info.get('priceToBook', None),
            'dividend_yield': info.get('dividendYield', None),
            'eps': info.get('trailingEps', None),
            'beta': info.get('beta', None),
            '52_week_high': info.get('fiftyTwoWeekHigh', None),
            '52_week_low': info.get('fiftyTwoWeekLow', None)
        }
        return json.dumps(fundamentals)
    except Exception as e:
        return f"Error: {e}"

def get_technical_indicators(symbol, workspace_dir):
    try:
        import yfinance as yf
        indicators = yf.Ticker(symbol).history(period="1mo")
        return str(indicators)
    except Exception as e:
        return f"Error: {e}"
"""

quant_tools_schemas = """    {
        "type": "function",
        "function": {
            "name": "get_current_stock_price",
            "description": "Get the current stock price for a given symbol.",
            "parameters": {
                "type": "object",
                "properties": {
                    "symbol": {"type": "string", "description": "The stock symbol."}
                },
                "required": ["symbol"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "get_stock_fundamentals",
            "description": "Get fundamental data for a given stock symbol using yfinance API.",
            "parameters": {
                "type": "object",
                "properties": {
                    "symbol": {"type": "string", "description": "The stock symbol."}
                },
                "required": ["symbol"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "get_technical_indicators",
            "description": "Get recent technical indicators and price history for a given stock symbol.",
            "parameters": {
                "type": "object",
                "properties": {
                    "symbol": {"type": "string", "description": "The stock symbol."}
                },
                "required": ["symbol"]
            }
        }
    },"""

content = content.replace('def read_url(url, workspace_dir):', quant_tools_code + '\ndef read_url(url, workspace_dir):')
content = content.replace('tools = [', 'tools = [\n' + quant_tools_schemas)

new_dispatch_logic = """                        elif func_name == "get_current_stock_price":
                            res = get_current_stock_price(args.get("symbol"), workspace_dir)
                        elif func_name == "get_stock_fundamentals":
                            res = get_stock_fundamentals(args.get("symbol"), workspace_dir)
                        elif func_name == "get_technical_indicators":
                            res = get_technical_indicators(args.get("symbol"), workspace_dir)
                        elif func_name == "read_file":"""
content = content.replace('elif func_name == "read_file":', new_dispatch_logic)

imports_old = 'from forge_utils import execute_terminal_command, read_file'
imports_new = 'from forge_utils import execute_terminal_command, get_current_stock_price, get_stock_fundamentals, get_technical_indicators, read_file'
content = content.replace(imports_old, imports_new)

sys_prompt_old = '''sys_prompt += """\n\nYou are an autonomous agent equipped with tools. You must use the tools to read the workspace, execute tests, and modify files.
You have full root access to a Debian terminal via `execute_terminal_command`. You can test your work by running standard compilation or execution commands for your assigned language (e.g. `node src/index.js`, `python src/main.py`, `go build`). Use `read_url` to look up documentation if you are stuck. Use `list_dir` to explore the workspace instead of guessing file paths.'''

sys_prompt_new = '''sys_prompt += """\n\nYou are a Quantitative Finance Expert equipped with financial analysis tools.
You have access to live market data via `get_current_stock_price`, `get_stock_fundamentals`, and `get_technical_indicators`. 
You also have full root access to a Debian terminal via `execute_terminal_command`. You MUST run `uv pip install yfinance pandas` before writing any python scripts that use those libraries!'''

content = content.replace(sys_prompt_old, sys_prompt_new)

with open(os.path.join(quant_dir, 'quant.py'), 'w', encoding='utf-8') as f:
    f.write(content)

print("Created quant.py successfully")
