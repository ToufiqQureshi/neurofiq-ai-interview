"""Find company job boards with SearXNG and hand them to the Job Map.

Search happens here; judgement does not. This reports the boards it found and
the directory decides what to store, running each one through the same guards
it has always used — fund and marketplace names, the board answering at all, a
ceiling on roles per employer, hiring in India, duplicates. Nothing here writes
to the database.

One source: a self-hosted SearXNG behind a residential proxy. Free, and with
polite pacing deeper than the metered API it replaced — one query walked five
pages returned 51 distinct companies against 45 for a paid Exa call on the same
slot. Nothing here spends money, so nothing here needs a budget guard.

Every run asks something different. The slot — provider, city, role — comes
from the clock: 6 providers x ~659 districts x 20 roles is around 79,000 slots,
so a two-minute cron runs for months before it repeats a question. City is a
district name (see load_india_districts), not a literal PIN code — a search
engine has nothing to match a 6-digit code against, since postings are never
tagged by one, but every district name here has actually appeared on one.

Nothing is remembered between runs, on purpose. A local list of boards already
sent grew to 1,827 entries and was skipping every one of them for good,
including boards the directory had only *temporarily* turned away — a company
whose website could not be resolved for free that day, say. The directory
already knows what it holds and says so in `rejected`; a second memory here can
only disagree with it, and when it does it hides boards rather than duplicating
them.

Usage:
    python discover_companies.py                    # one slot, push to the map
    python discover_companies.py --pages 5
    python discover_companies.py --city Bengaluru --provider lever
    python discover_companies.py --dry-run          # search only, push nothing
"""

import argparse
import json
import random
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

try:
    import pgeocode
except ImportError:
    pgeocode = None

SEARXNG_URL = "https://searxng-railway-production-bf85.up.railway.app/search"

# A browser string, because the engines behind SearXNG serve a CAPTCHA to
# anything that announces itself as a script.
BROWSER_UA = ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
              "(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

# Each provider: the hosts a search is restricted to, and how a board slug is
# read out of a result URL. Reading the board is the directory's job, not this
# script's — it already has an API client for all ten platforms.
PROVIDERS = {
    "greenhouse": {
        "hosts": ["boards.greenhouse.io", "job-boards.greenhouse.io"],
        "slug_re": re.compile(r"^https?://(?:boards|job-boards)\.greenhouse\.io/([^/?#]+)"),
    },
    "lever": {
        "hosts": ["jobs.lever.co"],
        "slug_re": re.compile(r"^https?://jobs\.lever\.co/([^/?#]+)"),
    },
    "keka": {
        "hosts": ["keka.com"],
        # Keka gives every customer a subdomain, so the slug is the label.
        "slug_re": re.compile(r"^https?://([^./]+)\.keka\.com"),
    },
    "ashby": {
        "hosts": ["jobs.ashbyhq.com"],
        "slug_re": re.compile(r"^https?://jobs\.ashbyhq\.com/([^/?#]+)"),
    },
    "workable": {
        "hosts": ["apply.workable.com"],
        "slug_re": re.compile(r"^https?://apply\.workable\.com/([^/?#]+)"),
    },
    "smartrecruiters": {
        # jobs.smartrecruiters.com is the platform's other public posting
        # host -- "jobs.smartrecruiters.com/altisource/744...-role-title"
        # carries the same company slug in the same position as
        # careers.smartrecruiters.com/altisource.
        "hosts": ["careers.smartrecruiters.com", "jobs.smartrecruiters.com"],
        "slug_re": re.compile(r"^https?://(?:careers|jobs)\.smartrecruiters\.com/([^/?#]+)"),
    },
    # The backend's admission pipeline (discovery_boards.go / jobs_sync.go)
    # already speaks all six of these -- this script just never asked them
    # anything. Restricted to India's two ATS platforms most were missing
    # before: Workday and Darwinbox are what large Indian employers actually
    # run when they are not on one of the six above.
    "darwinbox": {
        "hosts": ["darwinbox.in", "darwinbox.com"],
        "slug_re": re.compile(r"^https?://([^./]+)\.darwinbox\.(?:in|com)"),
    },
    "workday": {
        "hosts": ["myworkdayjobs.com"],
        # Stored as "tenant:region:site" -- the same three parts the job URLs
        # are built from (see boardURL's workday case backend-side), so a
        # single capture group is not enough here.
        "slug_re": re.compile(
            r"^https?://([^.]+)\.([^.]+)\.myworkdayjobs\.com/[^/]+/([^/?#]+)"),
        "slug_builder": lambda m: f"{m.group(1)}:{m.group(2)}:{m.group(3)}",
    },
    "recruitee": {
        "hosts": ["recruitee.com"],
        "slug_re": re.compile(r"^https?://([^./]+)\.recruitee\.com"),
    },
    "personio": {
        "hosts": ["jobs.personio.de", "jobs.personio.com"],
        # The slug IS the full host here, not a label -- Personio splits
        # tenants across .de and .com with no way to tell which without
        # having already matched it (see personioLinkRe backend-side).
        "slug_re": re.compile(r"^https?://([a-zA-Z0-9-]+\.jobs\.personio\.(?:de|com))"),
    },
    "freshteam": {
        "hosts": ["freshteam.com"],
        "slug_re": re.compile(r"^https?://([^./]+)\.freshteam\.com"),
    },
    "gem": {
        "hosts": ["jobs.gem.com"],
        "slug_re": re.compile(r"^https?://jobs\.gem\.com/([a-zA-Z0-9_-]+)"),
    },
}

# Path segments that match the board patterns but are the platform's own pages
# rather than an employer's board. keka.com returns www.keka.com/careers, which
# reads as a company called "www"; /j/ is Workable's per-job share URL and
# carries no account slug at all.
SKIP_SLUGS = {
    "", "api", "www", "app", "embed", "static", "assets", "search", "jobs",
    "job", "j", "careers", "career", "job-boards", "boards", "support", "help",
    "blog", "about", "login", "signup", "product", "pricing", "partners",
    "resources", "hire", "company", "companies", "contact", "privacy", "terms",
}

ROLES = [
    "Software Engineer", "Product Manager", "Data Scientist", "Backend Engineer",
    "Frontend Engineer", "Full Stack Engineer", "Mobile Developer",
    "Machine Learning Engineer", "QA Engineer", "Sales", "Marketing",
    "Designer", "DevOps Engineer", "Customer Success", "Business Analyst",
    "Data Analyst", "HR", "Finance", "Operations", "Project Manager",
]

# District-level, not city-level: a search engine returns nothing useful for a
# literal PIN code (postings are never tagged by one), and the ~128,000 raw
# place names GeoNames carries for India are mostly villages with no employer
# in them at all. A district is the coarsest unit that is still a real,
# searchable place name — every one of these has actually appeared on a job
# posting somewhere.
#
# Falls back to the original sixteen-city list (plus the bare "India") if
# pgeocode isn't installed, so this still runs without the extra dependency.
_FALLBACK_CITIES = [
    "Bengaluru", "Mumbai", "Gurgaon", "Hyderabad", "Noida", "Pune", "Chennai",
    "Delhi", "Ahmedabad", "Kochi", "Jaipur", "Chandigarh", "Indore",
    "Coimbatore", "Kolkata", "India",
]


def load_india_districts():
    if pgeocode is None:
        return list(_FALLBACK_CITIES)
    try:
        rows = pgeocode.Nominatim("in")._data
        districts = sorted({
            d.strip() for d in rows["county_name"].dropna().unique()
            if d and d.strip()
        })
    except Exception as e:
        print(f"pgeocode district lookup failed, using fallback city list: {e}",
              file=sys.stderr)
        return list(_FALLBACK_CITIES)
    # A bare "India" stays in the rotation for roles a board lists as
    # "Remote - India" or "India" rather than any one district.
    return districts + ["India"]


CITIES = load_india_districts()

# The districts that actually hold India's tech/startup employers. Sampling
# CITIES uniformly spends most of a run on districts like Sheohar or Nicobar,
# which genuinely have zero-to-a-handful of postings on any ATS -- a run of
# five rounds against random districts found almost nothing NEW, not because
# the search was broken, but because the automatic rotation had already found
# everything findable in those small districts (or there was never anything
# there to find). This list is what pick_weighted_city biases toward.
HUB_DISTRICTS = [
    "Bengaluru", "Bangalore Rural", "Mumbai", "Thane", "Pune", "Hyderabad",
    "Chennai", "Central Delhi", "New Delhi", "South Delhi", "West Delhi",
    "Gautam Buddha Nagar", "Gurgaon", "Faridabad", "Ahmedabad", "Vadodara",
    "Surat", "Kolkata", "Jaipur", "Chandigarh", "Indore", "Bhopal",
    "Coimbatore", "Ernakulam", "Thiruvananthapuram", "Kanpur Nagar",
    "Lucknow", "Nagpur", "Visakhapatnam", "India",
]
HUB_DISTRICTS = [d for d in HUB_DISTRICTS if d in CITIES]


def pick_weighted_city(hub_weight=0.7):
    """A hub district most of the time, any district the rest of the time --
    keeps the long-tail districts in rotation for eventual completeness
    without letting them dominate a run's time budget the way uniform random
    choice over 659 districts does."""
    if HUB_DISTRICTS and random.random() < hub_weight:
        return random.choice(HUB_DISTRICTS)
    return random.choice(CITIES)


PROVIDER_NAMES = list(PROVIDERS)


def slug_from_match(cfg, m):
    """Most providers are a single capture group; workday's board identity
    is three (tenant, region, site), so its cfg carries a slug_builder that
    composes them the same way the backend's boardURL does."""
    builder = cfg.get("slug_builder", lambda mm: mm.group(1))
    return builder(m)


def slot_from_clock(tick_seconds):
    """One (provider, city, role), chosen by the clock rather than remembered.

    Consecutive runs land on different work with nothing to keep in sync, and a
    second machine on the same cron asks the same question rather than drifting
    into its own private rotation.
    """
    idx = int(time.time() // max(tick_seconds, 1))
    provider = PROVIDER_NAMES[idx % len(PROVIDER_NAMES)]
    city = CITIES[(idx // len(PROVIDER_NAMES)) % len(CITIES)]
    role = ROLES[(idx // (len(PROVIDER_NAMES) * len(CITIES))) % len(ROLES)]
    return provider, city, role


def build_query(host, city, role):
    # site: is a real operator to SearXNG's engines, which are keyword engines,
    # and it is what keeps a page of results on one ATS host. (Exa, which this
    # replaced, is not a keyword engine — the same shape cost it half its
    # yield, 23 distinct companies against 45 for a plain sentence.)
    return f"site:{host} {role} {city} India"


def fetch_page(query, page, timeout):
    url = SEARXNG_URL + "?" + urllib.parse.urlencode(
        {"q": query, "format": "json", "pageno": page})
    req = urllib.request.Request(url, headers={"User-Agent": BROWSER_UA})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8", "ignore"))


def search(provider, city, role, pages, pause, timeout, verbose=True):
    """Walk pages until they stop paying, pausing like a reader between them.

    The pause is the whole reason this works. Five pages requested back to back
    put every upstream engine into CAPTCHA within about thirteen requests and
    the instance returned nothing at all for several minutes; the same five
    pages six to ten seconds apart returned 51 companies and tripped nothing.

    Returns (slugs, engine_trouble) so a run that found nothing can say whether
    the slot was empty or the instance was suspended — those need opposite
    responses and look identical in a count of zero.
    """
    cfg = PROVIDERS[provider]
    found, seen_urls, trouble = [], set(), []

    for host in cfg["hosts"]:
        query = build_query(host, city, role)
        for page in range(1, pages + 1):
            try:
                data = fetch_page(query, page, timeout)
            except Exception as e:
                print(f"  p{page}: {e}", file=sys.stderr)
                trouble.append(str(e))
                break

            results = data.get("results") or []
            for name, reason in (data.get("unresponsive_engines") or []):
                note = f"{name}: {reason}"
                if note not in trouble:
                    trouble.append(note)

            if not results:
                if verbose:
                    print(f"  p{page}: empty")
                break

            before = len(found)
            for r in results:
                url = r.get("url", "")
                if url in seen_urls:
                    continue
                seen_urls.add(url)
                m = cfg["slug_re"].match(url)
                if not m:
                    continue
                slug = slug_from_match(cfg, m).lower().strip("-")
                if slug in SKIP_SLUGS or len(slug) < 2 or ".." in slug:
                    continue
                if slug not in found:
                    found.append(slug)

            if verbose:
                print(f"  p{page}: {len(results):>3} results, +{len(found) - before} boards")
            if page < pages:
                time.sleep(random.uniform(*pause))

    return found, trouble


def push(provider, slugs, api, source):
    """Batched because admission reads every board live, and the server closes a
    response it has held past its 120s WriteTimeout — one 110-board push stored
    its companies and still reported a dropped connection, which reads exactly
    like having failed."""
    batch = 25
    url = api.rstrip("/") + "/api/admin/import-boards"
    totals = {"stored": 0, "attached": 0, "rejected": 0, "unnamed": 0}
    named = []

    for i in range(0, len(slugs), batch):
        payload = json.dumps({
            "source": source,
            "boards": [{"provider": provider, "slug": s} for s in slugs[i:i + batch]],
        }).encode()
        req = urllib.request.Request(
            url, data=payload, headers={"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=180) as resp:
                r = json.loads(resp.read().decode("utf-8", "ignore"))
        except urllib.error.HTTPError as e:
            body = e.read()[:160].decode("utf-8", "ignore")
            print(f"  push batch {i // batch + 1}: {e.code} {body}", file=sys.stderr)
            continue
        except Exception as e:
            print(f"  push batch {i // batch + 1}: {e}", file=sys.stderr)
            continue
        for k in totals:
            totals[k] += r.get(k, 0)
        named.extend(r.get("companies") or [])

    return totals, named


def main():
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--pages", type=int, default=8,
                    help="max SearXNG pages to walk per host (default 8) -- "
                         "search() already stops early the moment a page "
                         "comes back empty, so this is a ceiling, not a fixed count")
    ap.add_argument("--pause", default="3,8",
                    help="seconds between pages, min,max (default 3,8)")
    ap.add_argument("--timeout", type=int, default=60, help="per-request timeout")
    ap.add_argument("--max", type=int, default=100, help="most boards to report in one run")
    ap.add_argument("--tick", type=int, default=120, help="cron interval, for slot rotation")
    ap.add_argument("--provider", choices=PROVIDER_NAMES, help="pin instead of taking the slot")
    ap.add_argument("--city", help="pin instead of taking the slot")
    ap.add_argument("--role", help="pin instead of taking the slot")
    ap.add_argument("--dry-run", action="store_true", help="search only, push nothing")
    ap.add_argument("--api", default="http://localhost:8080")
    args = ap.parse_args()

    try:
        lo, hi = (float(x) for x in args.pause.split(","))
    except ValueError:
        sys.exit("--pause wants min,max seconds, e.g. 6,10")

    provider, city, role = slot_from_clock(args.tick)
    provider = args.provider or provider
    city = args.city or city
    role = args.role or role

    print(f"slot: {provider} / {city} / {role}")

    slugs, trouble = search(provider, city, role, args.pages, (lo, hi), args.timeout)
    fresh = slugs[:args.max]
    print(f"{len(slugs)} boards found, reporting {len(fresh)}")

    if not fresh:
        # An empty slot and a suspended instance both count zero, and they need
        # opposite responses: one is fine, the other means every engine behind
        # SearXNG is in CAPTCHA and the cron is walking slots for nothing.
        if trouble:
            print("no boards — search engines are unhappy:", "; ".join(trouble[:4]),
                  file=sys.stderr)
            sys.exit(1)
        print("no boards this slot (engines healthy)")
        return

    if args.dry_run:
        print("dry run:", ", ".join(fresh[:40]))
        return

    totals, named = push(provider, fresh, args.api, f"searxng-{city.lower()}")
    print(f"  stored   {totals['stored']}")
    print(f"  attached {totals['attached']}")
    print(f"  rejected {totals['rejected']}")
    print(f"  unnamed  {totals['unnamed']}")
    for n in named[:20]:
        print(f"    + {n}")


if __name__ == "__main__":
    main()
