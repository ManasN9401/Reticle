import os
import requests

# Load .env file manually to avoid dotenv dependency IDE warnings
if os.path.exists(".env"):
    with open(".env") as f:
        for line in f:
            if line.strip() and not line.startswith("#") and "=" in line:
                k, v = line.strip().split("=", 1)
                os.environ[k.strip()] = v.strip()

api_key = os.environ.get("GEMINI_API_KEY")
url = f"https://generativelanguage.googleapis.com/v1beta/models?key={api_key}"

try:
    response = requests.get(url)
    print("Status:", response.status_code)
    data = response.json()
    if "models" in data:
        for m in data["models"]:
            print(m["name"])
    else:
        print(data)
except Exception as e:
    print(e)
