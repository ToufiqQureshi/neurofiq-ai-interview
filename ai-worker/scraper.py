import os
from playwright.sync_api import sync_playwright

def scrape_url(url: str) -> str:
    """
    Uses Playwright to scrape a profile using a persistent profile.
    This bypasses basic anti-bot walls and keeps you logged in.
    """
    user_data_dir = os.path.join(os.getcwd(), "chrome_profile")
    
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch_persistent_context(
                user_data_dir=user_data_dir,
                headless=False, # Temporarily headed so user can see it or verify login
                args=["--disable-blink-features=AutomationControlled"]
            )
            
            page = browser.pages[0] if browser.pages else browser.new_page()
            
            # Navigate
            page.goto(url, wait_until="domcontentloaded", timeout=30000)
            
            # Wait a bit for dynamic content to load
            page.wait_for_timeout(3000)
            
            title = page.title()
            
            if "Sign in" in title or "Join LinkedIn" in title or "Login" in title:
                browser.close()
                return "ERROR_LOGIN_WALL_LINKEDIN"
                
            text = page.inner_text("body")
            browser.close()
            return text
            
    except Exception as e:
        print(f"Error scraping {url}: {e}")
        return ""
