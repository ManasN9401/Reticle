import os
import requests
from dotenv import load_dotenv
load_dotenv(".env")

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
