"""Visual demo: opens a real, visible Chromium window and drives it through
the exact same SearXNG queries discover_companies.py makes over its JSON API
-- so the search is literally watchable instead of terminal log lines.

Same SEARXNG_URL, same build_query, same slot picker as discover_companies.py
-- this is not a simulation, it is the identical query rendered as a page a
person can read instead of JSON a script parses.

Usage:
    python headful_demo.py
    python headful_demo.py --provider lever --city Bengaluru --role Designer --pages 4
    python headful_demo.py --watch-seconds 6   # longer pause per page to read it
"""

import argparse
import urllib.parse

from playwright.sync_api import sync_playwright

from discover_companies import PROVIDERS, SEARXNG_URL, build_query, slot_from_clock


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--provider", choices=list(PROVIDERS))
    ap.add_argument("--city")
    ap.add_argument("--role")
    ap.add_argument("--pages", type=int, default=4)
    ap.add_argument("--watch-seconds", type=float, default=5.0,
                     help="how long to leave each page on screen before moving on")
    args = ap.parse_args()

    provider, city, role = slot_from_clock(120)
    provider = args.provider or provider
    city = args.city or city
    role = args.role or role
    cfg = PROVIDERS[provider]
    host = cfg["hosts"][0]
    query = build_query(host, city, role)

    print(f"slot: {provider} / {city} / {role}")
    print(f"query: {query!r}")
    print(f"host restricted to: {host}")

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=False, args=["--start-maximized"])
        page = browser.new_page(no_viewport=True)

        for pageno in range(1, args.pages + 1):
            url = SEARXNG_URL.replace("/search", "") + "/search?" + urllib.parse.urlencode(
                {"q": query, "pageno": pageno})
            print(f"\n-> page {pageno}: {url}")
            page.goto(url, wait_until="domcontentloaded", timeout=30000)
            try:
                page.wait_for_selector("#urls .result, #results .result, .result",
                                        timeout=10000)
            except Exception:
                print("   (no results rendered on this page, or SearXNG returned empty)")
            count = page.locator("#urls .result, #results .result, .result").count()
            print(f"   {count} result cards visible on screen")
            page.wait_for_timeout(int(args.watch_seconds * 1000))

        print("\nDone -- leaving the browser open. Close the window when you're done looking.")
        try:
            # Blocks until the person closes the window, so the demo doesn't
            # vanish the instant the last page loads.
            page.wait_for_event("close", timeout=0)
        except Exception:
            pass


if __name__ == "__main__":
    main()
