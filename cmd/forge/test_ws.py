import asyncio
import websockets
import json
import os

async def run():
    with open(os.path.join(r"c:\Users\nathm\code\Reticle\.reticle", "control-token"), "r") as f:
        token = f.read().strip()
    headers = {"Authorization": f"Bearer {token}"}
    async with websockets.connect("ws://127.0.0.1:8081/ws", additional_headers=headers) as ws:
        await ws.send(json.dumps({
            "action": "enqueue",
            "prompt": "Build a modern landing page for a premium coffee brand called Aurora",
            "mode": "parallel",
            "effort": "auto",
            "group": ""
        }))
        print("Sent prompt!")
        
asyncio.run(run())
