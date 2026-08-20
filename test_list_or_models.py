import os
import requests
from dotenv import load_dotenv
load_dotenv(".env")

api_key = os.environ.get("OPENROUTER_API_KEY")
url = "https://openrouter.ai/api/v1/models"
headers = {"Authorization": f"Bearer {api_key}"}

try:
    response = requests.get(url, headers=headers)
    print("Status:", response.status_code)
    data = response.json()
    if "data" in data:
        for m in data["data"][:15]: # Just print the first 15 so we don't spam
            print(m["id"])
    else:
        print(data)
except Exception as e:
    print(e)
