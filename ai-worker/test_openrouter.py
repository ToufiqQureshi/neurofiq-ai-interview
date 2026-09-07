import os
import requests
from dotenv import load_dotenv

# Load from ../.env because we are in ai-worker
load_dotenv("../.env")
api_key = os.getenv("OPEN_ROUTER")

print(f"Testing OpenRouter API with key starting with {api_key[:10]}...")

try:
    response = requests.post(
        url="https://openrouter.ai/api/v1/chat/completions",
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json"
        },
        json={
            "model": "deepseek/deepseek-chat",
            "messages": [
                {"role": "user", "content": "Hello, are you working?"}
            ]
        }
    )
    print(f"Status Code: {response.status_code}")
    
    if response.status_code == 200:
        data = response.json()
        print("Success! Reply from model:")
        print(data['choices'][0]['message']['content'])
    else:
        print(f"Error Response: {response.text}")
except Exception as e:
    print(f"Exception occurred: {e}")
