import requests
from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from typing import List, Optional
from duckduckgo_search import DDGS
from deps import verify_internal_secret

router = APIRouter()

# ---- Pydantic models for Free Discovery ----
class FreeDiscoverPayload(BaseModel):
    query: str
    num_results: int = 30
    engine: Optional[str] = None

class SearchResult(BaseModel):
    Title: str
    URL: str

class FreeDiscoverResponse(BaseModel):
    engine: str
    results: List[SearchResult]

SEARXNG_INSTANCES = [
    "https://searx.be",
    "https://searx.tiekoetter.com",
    "https://priv.au",
    "https://search.inetol.net",
    "https://baresearch.org",
]

def search_searxng(query: str, num_results: int):
    for base in SEARXNG_INSTANCES:
        try:
            resp = requests.get(
                f"{base}/search",
                params={"q": query, "format": "json"},
                headers={"User-Agent": "Mozilla/5.0 (compatible; NeuroFIQ-JobMap/1.0)"},
                timeout=8,
            )
            if resp.status_code != 200:
                continue
            results = resp.json().get("results", [])
            if not results:
                continue
            return [
                {"title": r.get("title", ""), "href": r.get("url", "")}
                for r in results[:num_results]
            ]
        except Exception as e:
            print(f"SearXNG instance {base} failed: {e}")
            continue
    return []

@router.post("/discover-free", dependencies=[Depends(verify_internal_secret)], response_model=FreeDiscoverResponse)
async def discover_free(payload: FreeDiscoverPayload):
    try:
        raw_results = []
        engine_used = "none"

        if payload.engine != "searxng":
            try:
                ddgs = DDGS()
                raw_results = list(ddgs.text(payload.query, max_results=payload.num_results))
                if raw_results:
                    engine_used = "ddg"
            except Exception as e:
                print(f"DDG search failed: {e}")

        if not raw_results:
            raw_results = search_searxng(payload.query, payload.num_results)
            if raw_results:
                engine_used = "searxng"

        if not raw_results:
            return FreeDiscoverResponse(engine="none", results=[])

        seen = set()
        out = []
        for res in raw_results:
            url = res.get("href", "")
            if not url or url in seen:
                continue
            seen.add(url)
            out.append(SearchResult(Title=res.get("title", ""), URL=url))
            if len(out) >= payload.num_results:
                break

        return FreeDiscoverResponse(engine=engine_used, results=out)

    except Exception as e:
        print(f"Free Discovery Error: {e}")
        raise HTTPException(status_code=500, detail=str(e))
