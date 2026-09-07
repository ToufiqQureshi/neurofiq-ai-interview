import re

def clean_html_test(raw_html: str) -> str:
    # Remove unwanted tags completely (including their content)
    text = re.sub(r'<(script|style|code|noscript|svg)[^>]*>.*?</\1>', ' ', raw_html, flags=re.DOTALL | re.IGNORECASE)
    # Remove remaining HTML tags
    text = re.sub(r'<[^>]+>', ' ', text)
    # Remove extra whitespace
    text = re.sub(r'\s+', ' ', text).strip()
    return text

# Let's read the output from the last zenrows test to see how big it is
import requests
zenrows_key = "3c9247aa0b45ff0de9a1c58cf2e5188f45ccf035"
r = requests.get('https://api.zenrows.com/v1/', params={'apikey': zenrows_key, 'url': 'https://www.linkedin.com/in/toufiq-qureshi/', 'mode': 'auto'})
raw = r.text
print("Raw length:", len(raw))
cleaned = clean_html_test(raw)
print("Cleaned length:", len(cleaned))
print("First 500 chars:", cleaned[:500])
