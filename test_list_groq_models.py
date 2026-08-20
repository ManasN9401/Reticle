import os
import requests
from dotenv import load_dotenv
load_dotenv(".env")

api_key = os.environ.get("GROQ_API_KEY")
url = "https://api.groq.com/openai/v1/models"
headers = {"Authorization": f"Bearer {api_key}"}

try:
    response = requests.get(url, headers=headers)
    print("Status:", response.status_code)
    data = response.json()
    if "data" in data:
        for m in data["data"]:
            print(m["id"])
    else:
        print(data)
except Exception as e:
    print(e)
