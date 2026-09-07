import os
import requests
import json

api_key = "412fb5a5-f388-41e8-a7e9-57dffc02e710"

print("--- Testing BRD Test URL ---")
data = {
    "zone": "web_unlocker1",
    "url": "https://geo.brdtest.com/welcome.txt?product=unlocker&method=api",
    "format": "raw"
}

response = requests.post(
    "https://api.brightdata.com/request",
    json=data,
    headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"}
)
print(f"Status Code: {response.status_code}")
print(f"Response: {response.text}")

print("\n--- Testing LinkedIn URL ---")
data_ln = {
    "zone": "web_unlocker1",
    "url": "https://www.linkedin.com/in/toufiq-qureshi/",
    "format": "raw"
}
response_ln = requests.post(
    "https://api.brightdata.com/request",
    json=data_ln,
    headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"}
)
print(f"Status Code: {response_ln.status_code}")
print(f"Response length: {len(response_ln.text)}")
print(f"First 200 chars: {response_ln.text[:200]}")
