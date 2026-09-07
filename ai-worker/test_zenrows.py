import requests
import os

url = "https://www.linkedin.com/in/toufiq-qureshi/"
zenrows_key = "3c9247aa0b45ff0de9a1c58cf2e5188f45ccf035"

params = {
    'apikey': zenrows_key,
    'url': url,
    'mode': 'auto'
}

response = requests.get('https://api.zenrows.com/v1/', params=params)
print("Status:", response.status_code)
print("Length:", len(response.text))
if len(response.text) < 1000:
    print(response.text)
else:
    print(response.text[:500])
