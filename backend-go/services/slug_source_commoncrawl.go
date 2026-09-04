package services

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Common Crawl as a slug source.
//
// Common Crawl publishes a URL index of everything it crawled, about once a
// month, free and without a key. Asking it for every URL it has seen under
// jobs.lever.co or boards.greenhouse.io returns, in effect, a list of the
// boards on that host — because a board's public page is exactly the kind of
// page a crawler indexes.
//
// The numbers, measured against the 2026-34 index before this was written:
//
//	job-boards.greenhouse.io   3,954 slugs
//	apply.workable.com         3,157
//	jobs.ashbyhq.com           2,770
//	boards.greenhouse.io       1,781
//	*.myworkdayjobs.com        1,166
//	careers.smartrecruiters    415
//	*.keka.com                 206
//	*.darwinbox.in / .com      51
//	jobs.lever.co              1   <- see below
//	                          ------
//	                          13,501
//
// Lever is the one host this cannot serve. One index held a single block for
// jobs.lever.co, against six for Greenhouse and eight for Ashby, so Lever
// boards are essentially absent from the crawl. Discovery's search still finds
// them and must keep doing so; this source is an addition to that path, not a
// replacement for it.
//
// A second index is not a second copy of the first. Taking 2026-30 alongside
// 2026-34 added 204 Keka slugs the newer index did not have and 745
// Greenhouse ones — companies open and close boards, and each crawl catches a
// different moment. Reading several indexes is therefore worth more than
// reading one twice.

// commonCrawlCollections is where the list of published indexes lives.
const commonCrawlCollections = "https://index.commoncrawl.org/collinfo.json"

// ccMaxPagesPerHost bounds the page walk.
//
// The API pages in blocks and answers past the end with 400 or 404, so the
// walk normally stops on its own; this is the ceiling for the case where it
// does not. Eight covers every host measured — the widest, Greenhouse and
// Workday, held five pages each.
const ccMaxPagesPerHost = 8

// ccClient is separate from externalClient because a CDX page is a large
// streamed body rather than a small page fetch, and because these requests go
// to one host we chose ourselves rather than to a URL a third party supplied.
var ccClient = &http.Client{Timeout: 3 * time.Minute}

// ccHost is one queryable host and the rule for reading a slug out of its URLs.
type ccHost struct {
	// query is the url= parameter given to the CDX API.
	query string
	// provider is the ATS name job_service.FetchATSJobs switches on.
	provider string
	// slugFrom pulls the board identifier out of one crawled URL, or returns
	// "" when the URL is not a board page.
	slugFrom func(u string) string
}

// commonCrawlHosts covers every provider FetchATSJobs can read. Keeping the
// two lists in step is the same discipline boardSearchDomains needs: a board
// this can find but nothing can read is wasted work, and a board something can
// read but this never finds is only ever discovered by accident.
var commonCrawlHosts = []ccHost{
	{query: "job-boards.greenhouse.io/*", provider: "greenhouse", slugFrom: ccGreenhouseSlug},
	{query: "boards.greenhouse.io/*", provider: "greenhouse", slugFrom: ccGreenhouseSlug},
	{query: "jobs.lever.co/*", provider: "lever", slugFrom: ccPathSlug(leverLinkRe)},
	{query: "jobs.ashbyhq.com/*", provider: "ashby", slugFrom: ccPathSlug(ashbyLinkRe)},
	{query: "apply.workable.com/*", provider: "workable", slugFrom: ccPathSlug(workableLinkRe)},
	{query: "careers.smartrecruiters.com/*", provider: "smartrecruiters", slugFrom: ccPathSlug(smartRecruitersLinkRe)},
	{query: "*.keka.com", provider: "keka", slugFrom: ccSubdomainSlug("keka.com")},
	{query: "*.darwinbox.in", provider: "darwinbox", slugFrom: ccSubdomainSlug("darwinbox.in")},
	{query: "*.darwinbox.com", provider: "darwinbox", slugFrom: ccSubdomainSlug("darwinbox.com")},
	// Workday carries a tenant and a region in the URL but not the job-site id,
	// so what this emits is an incomplete slug — "tenant:region:" — and
	// admitCandidate resolves the missing third part against the live API.
	// See resolveWorkdaySlug for why that is done there rather than here.
	{query: "*.myworkdayjobs.com", provider: "workday", slugFrom: ccWorkdayPartialSlug},
}

// ccWorkdayPartialSlug reads the two parts of a Workday slug that are in the
// URL and leaves the third empty.
//
// A Workday board is <tenant>.<region>.myworkdayjobs.com/<site>, and the site
// id is not reliably in a crawled URL — the host names the tenant, the path
// may name a locale, a job, or nothing. So this returns "tenant:region:" and
// the resolution happens once, later, only for tenants that survive the free
// checks. Emitting a complete-looking slug here would mean probing the live
// API for all 1,166 crawled tenants during collection, which is the wrong
// place: collection makes no board calls.
func ccWorkdayPartialSlug(u string) string {
	m := workdayLinkRe.FindStringSubmatch(u)
	if m == nil {
		return ""
	}
	return m[1] + ":" + m[2] + ":"
}

// ccPathSlug reads the slug out of the URL path using the same regexes
// job_service uses on scraped HTML. Reusing them is the point: a URL the
// crawler saw and a URL a careers page linked are the same string, and a slug
// this source accepts must be one FetchATSJobs would accept too.
func ccPathSlug(re *regexp.Regexp) func(string) string {
	return func(u string) string {
		m := re.FindStringSubmatch(u)
		if m == nil {
			return ""
		}
		return m[1]
	}
}

// ccSubdomainSlug reads a tenant off a per-tenant host such as acme.keka.com.
func ccSubdomainSlug(suffix string) func(string) string {
	return func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		host := strings.ToLower(parsed.Hostname())
		if !strings.HasSuffix(host, "."+suffix) {
			return ""
		}
		return strings.TrimSuffix(host, "."+suffix)
	}
}

// ccRecord is the one field this needs out of a CDX row.
type ccRecord struct {
	URL string `json:"url"`
}

// LatestCommonCrawlIndexes returns the n most recent index ids, newest first.
//
// The ids are dated (CC-MAIN-2026-34) and the collection list is ordered, so
// "recent" needs no parsing beyond taking the front of the list.
func LatestCommonCrawlIndexes(n int) ([]string, error) {
	resp, err := ccClient.Get(commonCrawlCollections)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("common crawl collinfo status %d", resp.StatusCode)
	}
	body, err := ReadCapped(resp.Body, 4<<20)
	if err != nil {
		return nil, err
	}

	var collections []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &collections); err != nil {
		return nil, err
	}
	if n <= 0 || n > len(collections) {
		n = len(collections)
	}
	ids := make([]string, 0, n)
	for _, c := range collections[:n] {
		ids = append(ids, c.ID)
	}
	return ids, nil
}

// HarvestFromCommonCrawl reads the given indexes and returns the board
// candidates it found. It makes no board calls and writes nothing — deciding
// what to keep is HarvestSlugs's job.
func HarvestFromCommonCrawl(indexes []string) []slugCandidate {
	seen := make(map[string]bool)
	var out []slugCandidate

	for _, index := range indexes {
		for _, host := range commonCrawlHosts {
			found, rows, failed := 0, 0, 0
			for page := 0; page < ccMaxPagesPerHost; page++ {
				if page > 0 {
					time.Sleep(ccPageGap)
				}
				urls, more, err := ccFetchPage(index, host.query, page)
				if err != nil {
					failed++
					// An empty page that survived its retries is the host being
					// genuinely out of data, so stop. Anything else is one bad
					// page: the first version broke here, which is how a single
					// 504 on page 1 cost every page after it — Ashby collected
					// 345 slugs where its two pages hold 2,955.
					if errors.Is(err, errEmptyPage) {
						log.Printf("common crawl: %s %s page %d empty after retries, stopping",
							index, host.query, page)
						break
					}
					log.Printf("common crawl: %s %s page %d failed, continuing: %v",
						index, host.query, page, err)
					continue
				}
				rows += len(urls)
				for _, u := range urls {
					slug := host.slugFrom(u)
					if slug == "" {
						continue
					}
					slug = strings.TrimSpace(slug)
					if !validATSSlug(slug) || !boardSlugIsAdmissible(slug) {
						continue
					}
					key := boardKey(host.provider, slug)
					if seen[key] {
						continue
					}
					seen[key] = true
					found++
					out = append(out, slugCandidate{
						Provider: host.provider,
						Slug:     slug,
						Source:   SourceCommonCrawl,
					})
				}
				if !more {
					break
				}
			}
			// Rows and failures, not just the slug count. A host that returns
			// far fewer slugs than usual is either genuinely smaller or was
			// half-read, and the slug count alone cannot tell those apart —
			// which is exactly the ambiguity that hid the bug above.
			log.Printf("common crawl: %s %s -> %d new slugs (%d rows, %d pages failed)",
				index, host.query, found, rows, failed)
			time.Sleep(ccPageGap)
		}
	}
	return out
}

// SourceCommonCrawl is written to companies.source for rows this path stored.
const SourceCommonCrawl = "common-crawl"

// ccPageGap is the pause between CDX page requests.
//
// Not politeness for its own sake — measured. Walking the pages back to back
// had the API answer 504 on the second request for job-boards.greenhouse.io
// and on the first for boards.greenhouse.io, which ended each walk early and
// cost most of the slugs: 748 collected where a paced run finds 3,954, and
// zero for the host that failed outright.
const ccPageGap = 2 * time.Second

// ccRetries is how many times a page is re-requested after a gateway error.
//
// The failures above were 504s, which is the index saying it is busy rather
// than that the page does not exist. Retrying is what separates "this host has
// no more pages" from "ask again in a moment", and getting that wrong silently
// truncates the harvest to whatever the first request happened to return.
const ccRetries = 3

// ccFetchPage reads one CDX page and reports whether another may follow.
//
// The API streams newline-delimited JSON. It answers a request past the last
// page with 404 or 400 — both mean the walk is over, not that something broke,
// and treating either as an error would report a completed walk as a failure.
// A 5xx is the opposite: the page exists and the index is busy, so it is worth
// asking again before giving up on the rest of the host.
func ccFetchPage(index, query string, page int) ([]string, bool, error) {
	var lastErr error
	for attempt := 0; attempt < ccRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * ccPageGap)
		}
		urls, more, retryable, err := ccFetchPageOnce(index, query, page)
		if err == nil {
			return urls, more, nil
		}
		lastErr = err
		if !retryable {
			return nil, false, err
		}
	}
	return nil, false, lastErr
}

// ccFetchPageOnce is one attempt. retryable separates a busy index from a
// request that will fail the same way however often it is repeated.
func ccFetchPageOnce(index, query string, page int) (urls []string, more, retryable bool, err error) {
	endpoint := fmt.Sprintf(
		"https://index.commoncrawl.org/%s-index?url=%s&output=json&page=%d",
		url.PathEscape(index), url.QueryEscape(query), page,
	)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, false, err
	}
	req.Header.Set("User-Agent", "NeuroFIQ-JobMap/1.0 (+https://neurofiq.in)")

	resp, err := ccClient.Do(req)
	if err != nil {
		return nil, false, true, err
	}
	defer resp.Body.Close()

	// Past the last page the API answers 404, and 400 for a page number it
	// considers out of range. Both are the end of the walk.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
		return nil, false, false, nil
	}
	if resp.StatusCode >= 500 {
		return nil, false, true, fmt.Errorf("cdx status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, false, fmt.Errorf("cdx status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec ccRecord
		if jsonErr := json.Unmarshal(line, &rec); jsonErr != nil {
			continue
		}
		if rec.URL != "" {
			urls = append(urls, rec.URL)
		}
	}
	// A truncated body is worth another attempt: the rows already read are a
	// partial page, and keeping them would silently shorten the walk.
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, false, true, scanErr
	}
	// A 200 carrying no rows is not the end of the walk.
	//
	// The end is 400 or 404, handled above. This is the index answering
	// successfully and returning nothing, which it does under load — and
	// treating it as the end is how Ashby stopped after one page: 13,000 rows
	// read of the 20,412 its two pages hold, with nothing logged as failed
	// because nothing had failed. Retrying is what separates a busy moment
	// from a genuinely exhausted host.
	if len(urls) == 0 {
		return nil, false, true, errEmptyPage
	}
	return urls, true, false, nil
}

// errEmptyPage is a 200 that carried no rows — retryable, and distinct from
// the 400/404 that genuinely ends a walk.
var errEmptyPage = errors.New("cdx returned an empty page")

// ccGreenhouseSlug reads a Greenhouse slug out of a crawled URL.
//
// greenhouseLinkRe accepts both shapes Greenhouse serves — the linked board
// at /<slug> and the embedded one at /embed/job_board?for=<slug> — and fills
// one capture group per shape, so the slug is whichever group is not empty.
// Reading group 1 unconditionally returned "embed" for every embedding
// company, which nonSlugSegments then rejected: the company was never found
// rather than found wrong. Observe.ai surfaced it during the register run.
func ccGreenhouseSlug(u string) string {
	m := greenhouseLinkRe.FindStringSubmatch(u)
	if m == nil {
		return ""
	}
	return firstGroup(m)
}
