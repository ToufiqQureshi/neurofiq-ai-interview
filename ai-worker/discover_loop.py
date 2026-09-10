"""Continuous discovery: a fixed number of workers running in parallel, each
walking its own (provider, city) all the way through SearXNG's pages, all
sharing one global pacer so the combined request rate to the shared SearXNG
instance stays inside the safe range regardless of worker count (see
test_parallel_discovery.py's docstring for why per-worker pacing alone is not
enough).

No role term in the query -- see discover_companies.slot_from_clock's
docstring: a role roughly halves the unique boards a query finds, for the
occasional company a role-less query would have missed anyway.

Once every worker in a round has walked every page it has (or hit the --pages
ceiling), the round is done: results are pushed to the Job Map API, and the
loop pauses --pause-between seconds before starting the next round with a
fresh set of (provider, city) combos.

Usage:
    python discover_loop.py                       # 2 workers, forever, push to API
    python discover_loop.py --workers 2 --pause-between 180
    python discover_loop.py --rounds 3             # stop after 3 rounds
    python discover_loop.py --dry-run              # search only, never push
"""

import argparse
import random
import threading
import time

from discover_companies import (
    PROVIDER_NAMES, PROVIDERS, SKIP_SLUGS,
    build_query, fetch_page, pick_weighted_city, push, slug_from_match,
)

MIN_GAP = (3.0, 8.0)  # global gap between ANY two requests to SearXNG


class GlobalPacer:
    def __init__(self, min_gap):
        self.min_gap = min_gap
        self._lock = threading.Lock()
        self._last = 0.0

    def wait_turn(self):
        with self._lock:
            now = time.monotonic()
            gap = random.uniform(*self.min_gap)
            sleep_for = (self._last + gap) - now
            if sleep_for > 0:
                time.sleep(sleep_for)
            self._last = time.monotonic()


def walk_all_pages(pacer, worker_id, provider, city, max_pages, timeout):
    """Walks every page a (provider, city) query has, stopping the moment a
    page comes back empty -- max_pages is a ceiling, not a target."""
    cfg = PROVIDERS[provider]
    found, seen_urls = [], set()

    for host in cfg["hosts"]:
        query = build_query(host, city)
        page = 1
        while page <= max_pages:
            pacer.wait_turn()
            try:
                data = fetch_page(query, page, timeout)
            except Exception as e:
                print(f"[w{worker_id}] {provider}/{city} p{page}: error -- {e}")
                break

            trouble = data.get("unresponsive_engines") or []
            hits = data.get("results") or []
            if not hits:
                print(f"[w{worker_id}] {provider}/{city} p{page}: "
                      f"empty, all pages walked ({len(found)} boards)")
                break

            before = len(found)
            for r in hits:
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

            note = f" (engine trouble: {trouble})" if trouble else ""
            print(f"[w{worker_id}] {provider}/{city} p{page}: "
                  f"{len(hits)} results, +{len(found) - before} boards, "
                  f"page -> {page + 1}{note}")
            page += 1

    return found


def pick_combos(n):
    providers = random.sample(PROVIDER_NAMES, min(n, len(PROVIDER_NAMES)))
    while len(providers) < n:
        providers.append(random.choice(PROVIDER_NAMES))
    return [(p, pick_weighted_city()) for p in providers]


def run_round(round_num, n_workers, max_pages, timeout):
    pacer = GlobalPacer(MIN_GAP)
    combos = pick_combos(n_workers)
    print(f"\n=== round {round_num}: {n_workers} workers ===")
    for i, (p, c) in enumerate(combos):
        print(f"  w{i}: {p} / {c}")

    per_worker = {}
    lock = threading.Lock()

    def target(i, provider, city):
        slugs = walk_all_pages(pacer, i, provider, city, max_pages, timeout)
        with lock:
            per_worker[i] = (provider, slugs)

    threads = [
        threading.Thread(target=target, args=(i, p, c))
        for i, (p, c) in enumerate(combos)
    ]
    t0 = time.monotonic()
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    elapsed = time.monotonic() - t0

    print(f"--- round {round_num} done in {elapsed:.1f}s: "
          f"all {n_workers} workers walked every page they had ---")
    return per_worker


def main():
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--workers", type=int, default=2)
    ap.add_argument("--pages", type=int, default=8, help="ceiling per host (default 8)")
    ap.add_argument("--timeout", type=int, default=60)
    ap.add_argument("--pause-between", type=int, default=180,
                     help="seconds to wait after a round finishes, before the next (default 180 = 3 min)")
    ap.add_argument("--rounds", type=int, default=0, help="stop after N rounds (0 = forever)")
    ap.add_argument("--api", default="http://localhost:8080")
    ap.add_argument("--dry-run", action="store_true", help="search only, push nothing")
    args = ap.parse_args()

    round_num = 0
    try:
        while True:
            round_num += 1
            per_worker = run_round(round_num, args.workers, args.pages, args.timeout)

            for i, (provider, slugs) in per_worker.items():
                if not slugs:
                    continue
                if args.dry_run:
                    print(f"  w{i} ({provider}) dry run: {', '.join(slugs[:40])}")
                    continue
                totals, named = push(provider, slugs, args.api, source=f"discover-loop-w{i}")
                print(f"  w{i} ({provider}) pushed {len(slugs)} boards -> "
                      f"stored {totals['stored']}, attached {totals['attached']}, "
                      f"rejected {totals['rejected']}, unnamed {totals['unnamed']}")
                for n in named[:10]:
                    print(f"    + {n}")

            if args.rounds and round_num >= args.rounds:
                print(f"\nreached --rounds {args.rounds}, stopping")
                break

            print(f"\n...pausing {args.pause_between}s before the next round...")
            time.sleep(args.pause_between)
    except KeyboardInterrupt:
        print("\nstopped by user")


if __name__ == "__main__":
    main()
