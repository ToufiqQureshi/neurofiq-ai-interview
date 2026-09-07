import os
import requests
import re
from bs4 import BeautifulSoup

def clean_html(raw_html: str) -> str:
    try:
        soup = BeautifulSoup(raw_html, "html.parser")
        for tag in soup(["script", "style", "code", "noscript", "svg", "nav", "footer", "meta", "link"]):
            tag.decompose()
        text = soup.get_text(separator=" ", strip=True)
        text = re.sub(r'\s+', ' ', text).strip()
        return text
    except Exception as e:
        print(f"BeautifulSoup parsing error: {e}")
        # Fallback to aggressive regex
        text = re.sub(r'<(script|style|code|noscript|svg)[^>]*>.*?</\1>', ' ', raw_html, flags=re.DOTALL | re.IGNORECASE)
        text = re.sub(r'<[^>]+>', ' ', text)
        return re.sub(r'\s+', ' ', text).strip()
def scrape_url(url: str) -> str:
    """
    Uses Firecrawl to scrape a profile URL.
    Falls back to Bright Data Web Unlocker API if Firecrawl hits a login wall or fails.
    """
    firecrawl_key = os.getenv("FIRECRAWL_API_KEY")
    brightdata_key = os.getenv("BRIGHTDATA_API_KEY")
    brightdata_zone = os.getenv("BRIGHTDATA_ZONE", "web_unlocker1")
    scraperapi_key = os.getenv("SCRAPERAPI_KEY")
    zenrows_key = os.getenv("ZENROWS_KEY")
    
    # 1. Try Firecrawl
    if firecrawl_key:
        print(f"Trying Firecrawl for {url}...")
        try:
            response = requests.post(
                "https://api.firecrawl.dev/v1/scrape",
                headers={
                    "Authorization": f"Bearer {firecrawl_key}",
                    "Content-Type": "application/json"
                },
                json={
                    "url": url,
                    "formats": ["markdown"],
                    "onlyMainContent": True
                },
                timeout=30
            )
            
            if response.status_code == 200:
                data = response.json()
                if data.get("success"):
                    markdown = data.get("data", {}).get("markdown", "")
                    
                    # Basic check for LinkedIn login wall
                    if "linkedin.com" in url and ("LinkedIn Login" in markdown or "Sign In" in markdown) and len(markdown) < 2000:
                        print("Firecrawl hit LinkedIn login wall. Falling back to ZenRows...")
                    else:
                        print("Firecrawl succeeded.")
                        return markdown
                else:
                    print(f"Firecrawl API returned success=False for {url}: {data}")
            else:
                print(f"Firecrawl API error {response.status_code} for {url}: {response.text}")
                
        except Exception as e:
            print(f"Error scraping {url} with Firecrawl: {e}")
            
    # 2. Fallback to ZenRows
    if zenrows_key:
        print(f"Trying ZenRows for {url}...")
        try:
            payload = {'apikey': zenrows_key, 'url': url, 'mode': 'auto'}
            z_response = requests.get('https://api.zenrows.com/v1/', params=payload, timeout=60)
            if z_response.status_code == 200:
                html = z_response.text
                if "linkedin.com" in url and ("<title>LinkedIn Login" in html or "authwall" in html):
                    print("ZenRows hit LinkedIn login wall. Falling back to ScraperAPI...")
                else:
                    print(f"ZenRows succeeded. Raw HTML length: {len(html)}")
                    return clean_html(html)
            else:
                print(f"ZenRows error {z_response.status_code}: {z_response.text}")
        except Exception as e:
            print(f"Error scraping {url} with ZenRows: {e}")

    # 3. Fallback to ScraperAPI
    if scraperapi_key:
        print(f"Trying ScraperAPI for {url}...")
        try:
            payload = {'api_key': scraperapi_key, 'url': url, 'render': 'true'}
            s_response = requests.get('https://api.scraperapi.com/', params=payload, timeout=60)
            if s_response.status_code == 200:
                html = s_response.text
                if "linkedin.com" in url and ("<title>LinkedIn Login" in html or "authwall" in html):
                    print("ScraperAPI hit LinkedIn login wall. Falling back to Bright Data...")
                else:
                    print(f"ScraperAPI succeeded. Raw HTML length: {len(html)}")
                    return clean_html(html)
            else:
                print(f"ScraperAPI error {s_response.status_code}: {s_response.text}")
        except Exception as e:
            print(f"Error scraping {url} with ScraperAPI: {e}")

    # 4. Fallback to Bright Data Web Unlocker
    print(f"Trying Bright Data Web Unlocker for {url}...")
    try:
        bd_response = requests.post(
            "https://api.brightdata.com/request",
            headers={
                "Authorization": f"Bearer {brightdata_key}",
                "Content-Type": "application/json"
            },
            json={
                "zone": brightdata_zone,
                "url": url,
                "format": "raw"
            },
            timeout=60
        )
        
        if bd_response.status_code == 200:
            html = bd_response.text
            if "linkedin.com" in url and ("<title>LinkedIn Login" in html or "authwall" in html):
                print("Bright Data also hit LinkedIn login wall.")
                return "ERROR_LOGIN_WALL_LINKEDIN"
            print(f"Bright Data succeeded. Raw HTML length: {len(html)}")
            cleaned_text = clean_html(html)
            print(f"Cleaned text length: {len(cleaned_text)}")
            return cleaned_text
        else:
            print(f"Bright Data API error {bd_response.status_code}: {bd_response.text}")
            return "ERROR_LOGIN_WALL_LINKEDIN"
            
    except Exception as e:
        print(f"Error scraping {url} with Bright Data: {e}")
        return ""
