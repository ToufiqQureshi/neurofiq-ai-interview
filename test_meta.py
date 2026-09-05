import urllib.request
import re

headers = {'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)'}
urls = [
    'https://porter.in',
    'https://tekion.com/',
    'https://business.paytm.com/',
    'https://www.databricks.com/',
    'https://vyaparapp.in',
    'https://www.capco.com/',
    'https://www.purestorage.com'
]

for url in urls:
    try:
        req = urllib.request.Request(url, headers=headers)
        with urllib.request.urlopen(req, timeout=5) as resp:
            text = resp.read().decode('utf-8', errors='ignore')[:100000]
            m = re.search(r'<meta[^>]+(?:name|property)=["\'](?:description|og:description)["\'][^>]+content=["\']([^"\']+)["\']', text, re.I)
            if not m:
                m = re.search(r'<meta[^>]+content=["\']([^"\']+)["\'][^>]+(?:name|property)=["\'](?:description|og:description)["\']', text, re.I)
            print(url, '-->', m.group(1).strip() if m else 'NOT FOUND')
    except Exception as e:
        print(url, '--> ERR:', e)
