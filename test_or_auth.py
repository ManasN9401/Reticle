import os
import requests
import json

key = os.environ.get("OPENROUTER_API_KEY")
if not key:
    with open(".env", "r") as f:
        for line in f:
            if line.startswith("OPENROUTER_API_KEY="):
                key = line.strip().split("=")[1]
                break

resp = requests.get("https://openrouter.ai/api/v1/auth/key", headers={"Authorization": f"Bearer {key}"})
print(resp.status_code)
print(json.dumps(resp.json(), indent=2))
