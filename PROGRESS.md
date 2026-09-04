# Progress Log

## 2026-09-03
- **Slug harvesting: boards without paying a search for each one (`slug_harvest.go`, `slug_source_commoncrawl.go`, `slug_source_register.go`)**: Discovery costs 2.02 metered searches per company — 293 of Exa's 800 monthly calls bought 145 companies in September, capping the month at roughly 400 however fast the cron runs. But boards are public pages a crawler has already indexed, so the list can be read instead of rediscovered: one Common Crawl index yields 13,501 slugs across the eight readable providers for ~20 requests and no metered call. Admission is unchanged — a harvested slug is a candidate that must pass the same gate a search hit does (`FetchATSJobs`, `maxBoardRoles`, `firstIndianLocation`, `boardRowIsAdmissible`, dedupe). Two sources feed one gate: Common Crawl for jobs, the startup register for the sector/stage/coordinates no board API reports.
- **Bug in the first run: the page walk stopped on the first CDX error.** Requesting pages back to back had Common Crawl answer 504, and a 504 was treated as "no more pages" — 748 Greenhouse slugs collected where a paced walk finds 3,954, and zero for the host whose *first* page failed. A 400 is genuinely the end of the walk; a 5xx is the index saying it is busy. Now separated, with three retries and a 2s gap between pages.
- **`looksIndian` strengthened for harvest scale.** Added the 36 states and UTs, because boards write "Mumbai MH" and "Kutch - Gujarat" and those were read as not-Indian — which rejects the whole company when they are its only Indian roles. Added a foreign-marker rule for the opposite failure: "Bangalore, Mexico" is a real row and passed the city test. A foreign country now disqualifies a location only when nothing in it names India or an Indian state, so "Bengaluru, Karnataka, India; Pleasanton, United States" still counts. Measured against 30,000 real board postings: 0.1% false negatives before the states, 0.37% carrying a foreign marker.
- **Talent-pool postings are not vacancies.** The harvest's first run surfaced Affinidi's "Be Part of our Talent Community" as that company's only Indian role. It is a posting by every structural measure — own id, own apply URL, a location — so nothing upstream rejected it. `looksLikeRoleTitle` now matches the phrase, narrowly enough that "Talent Acquisition Specialist" and "Application Security Engineer" survive.
- **`findDuplicateCompany` does not scale to a harvest.** It reads the whole companies table per call, which is free at five calls a tick and a table scan per candidate at 13,501. The harvest builds the same index once (`directoryIndex`) and updates it as the run goes, so two candidates for one business inside a single run cannot both be written.
- **Live ATS Feed Engine & Global Search API (`GET /api/jobs`)**: Added high-performance PostgreSQL query in Go backend (`services.ListGlobalJobs`) searching 3,550+ active jobs across 212 verified hiring companies with intelligent tech synonyms (`Golang` -> `Go/Backend`, `AI/ML` -> `Data/Machine Learning`), location filtering, and pagination.
- **Joblet.ai-Inspired Job Discovery Portal (`/jobs`)**: Built and launched an ultra-premium 3-column discovery portal featuring luxury editorial typography (`Fraunces` / `Newsreader` serif), subtle background grid lines, horizontal trending roles carousel, and real-time ATS verified feeds.
- **On-Demand Floating Company Drawer (`CompanyDrawer.tsx`)**: Integrated on-demand `/api/companies/:id` fetching into fixed overlay drawer (`z-[999]`) showing live hiring counts, active role accordion, direct careers link, and instant AI mock interview CTA.
- **Unified Multi-Modal Search Capsule (`UnifiedSearchCapsule.tsx`)**: Integrated text/keyword search, browser-native Voice Recognition (Web Speech API), and tech sub-hub location dropdown into a single floating capsule.
- **Interactive Natural-Language Preference Filter (`PreferenceSentenceFilter.tsx`)**: Conversational sentence builder with dynamic dropdown highlights (*"I am a [Role] looking for [Experience] [WorkType] in [Location] ➔"*).
- **1-Click AI Mock Interview CTA Integration**: Linked real company job cards directly to Neurofiq's tailored AI mock interview engine (FastAPI + DeepSeek) passing pre-filled role and company context (`/dashboard?practice_job=...&company=...`).).

## 2026-09-02
- Formatted entire Go codebase with `gofmt -w .` to resolve GitHub Actions CI lint failure (`gofmt -l .`).
- Merged `job-map-data-integrity` into `main` and pushed to GitHub: encompasses pan-India role attribution guards, whole-word location matching, boot/cron `ReapplyGuards` cleanup, homepage-based metadata enrichment, and Profile Radar (`/radar`) feature.
- Installed and activated `claude-mem` v13.23.1 with Antigravity CLI lifecycle hooks, MCP integration, and background worker daemon.

## 2026-09-01
- Created **Production Checklist & High-Scale Architecture Guide (`PRODUCTION_CHECKLIST.md`)**: comprehensive roadmap covering Redis caching, PgBouncer connection pooling, Asynq background task queues, distributed token-bucket rate limiting, PostgreSQL read replicas, and Prometheus/Sentry observability for scaling to millions of users.
- Built **Floating Company Inspector Drawer (`CompanyDrawer.tsx`)** inspired by BangaloreStartupMap / Airbnb: clicking any startup pin slides in a rich details card on the right containing company bio, funding stage, direct career/website links, active job roles, and 1-Click AI Mock Interview CTA.
- Optimized Map Layout & Above-the-Fold Viewport: shifted stat cards to Grid-only mode so 2D and 3D map views load immediately visible with zero vertical scroll required.
- Added **Interactive Open Roles Popup Accordion / Toggle (`CompanyJobList.tsx`)** to both 2D Leaflet and 3D MapLibre popups: candidates can click on `[ 💼 N open roles ▼ ]` directly on any map marker to expand the list of active job postings with 1-Click AI Mock Interview triggers and direct career application links.
- Cleaned up Map controls: removed duplicate internal 2D/3D toggle from MapLibre canvas, establishing the top-right header `[ ⊞ Grid | 🌐 2D Map | ✨ 3D Map ]` switcher as the single source of truth.
- Added 3-Way Directory View Switcher (`[ ⊞ Grid | 🌐 2D Map | ✨ 3D Map ]`) allowing candidates and recruiters to toggle between classic Grid Cards, standard 2D Leaflet map, and GPU-accelerated MapLibre GL 3D perspective.
- Integrated **MapLibre GL (`maplibre-gl`) WebGL 3D Map Engine** (`MapLibreCompanyMap.tsx`) with 55° camera tilt perspective, 3D compass navigation, 2D/3D angle toggle, and city boundary locking.
- Implemented Strict City Boundary Locking & Zoom Clamping: configured `maxBounds` with `[lng, lat]` format and `minZoom` per Tech Hub so users cannot pan outside or zoom out beyond the chosen urban tech corridor.
- Built Dead Jobs Auto-Pruner (`PruneDeadJobs` & `POST /api/jobs/prune-dead`) with concurrent HTTP link validation, automatically scanning active job URLs and purging 404/410/expired postings from the database.
- Implemented BangaloreStartupMap-style precision tech sub-hub geocoding across major Indian tech clusters (HSR Layout, Koramangala, Indiranagar, Bellandur, Whitefield, EGL Domlur, Electronic City, Cyber City, BKC, Powai, Hinjawadi, etc.), eliminating single-point marker stacking.
- Configured Leaflet MarkerCluster with dynamic city-zoom unclustering (`disableClusteringAtZoom={11}`, `maxClusterRadius={25}`) to display individual circular company logo pins across urban neighborhoods.
- Added live "Clean Dead Jobs" instant action button on directory header with feedback toasts.
- Implemented Pan-India Tech Hub filter switcher (All Hubs, Bengaluru, Delhi NCR, Mumbai, Hyderabad, Pune) with instant viewport flying and dynamic cluster zoom.
- Upgraded Leaflet map to 3D-styled CartoDB/OpenStreetMap tiles and added 1-Click "🎯 Practice AI Mock Interview" action within map popups and job listings.
- Review pass on the board-discovery branch, six fixes: `resolveCompanyWebsite` fallback to `WebSearch`, link scan department filters, empty read retry guards, and rate-limit controls for `POST /api/companies/discover`.

## 2026-08-31
- **Email + Password Authentication & Bcrypt Password Hashing**: Implemented `/auth/register` and `/auth/login` endpoints in Go with `bcrypt.GenerateFromPassword` and `bcrypt.CompareHashAndPassword`, setting session cookies on registration and login.
- **Candidate Onboarding Flow (`/onboarding`)**: Created a 3-step interactive onboarding wizard (`Onboarding.tsx`) allowing candidates to select their Experience Level (Fresher, Mid-Level, Senior), College/Company, Target Role, Tech Stack badges, LinkedIn profile, and Interview Goals.
- **Database Schema & Route Guarding**: Updated `models.User` in Go with `PasswordHash`, `FullName`, `ExperienceLevel`, `TargetRole`, `TechStack`, `LinkedInURL`, `IsOnboarded`, and nullable `GithubID`. Configured `ProtectedRoute` in React to automatically redirect un-onboarded candidates to `/onboarding`.
- **Automated Playwright E2E Test Suite**: Ran full browser verification covering signup -> onboarding wizard -> dashboard landing -> logout -> direct dashboard login with 100% pass rate.
- **Company Directory Stats Endpoint**: Registered `GET /api/companies/stats` before `GET /api/companies/:id` in `main.go` to provide global directory metrics (total companies, hiring companies, open jobs, fresh postings).
- Configured Free Claude Code proxy (`http://127.0.0.1:8082/admin`) with OpenRouter API integration and set default routing model to `openrouter/z-ai/glm-5.3-flash`.
- Configured OpenCode (`~/.config/opencode/opencode.json`) with OpenRouter API integration for GLM models (`z-ai/glm-5.3-flash` and `z-ai/glm-5.2`).
- Merged the launch-hardening branch, minus the recruiter invite funnel: sharing
  is candidate-owned, so a recruiter minting invite links was the wrong
  direction. Removed 1,118 lines across 8 files plus its wiring; the share slug
  and public report page were untouched and the invite was already optional at
  submit, so nothing in the candidate path depended on it.
- Found the real reason the directory carried dead companies. Of the 26 stored
  with no careers page at all, 62% were D2C and 88% were pre-Series-A: the
  discovery agent was returning small Shopify storefronts, which have no careers
  page because they do not hire. Three fixes: a post-hook on the discovery agent
  that drops aggregator URLs (LinkedIn, Crunchbase, Tracxn) and duplicate hosts
  and raises so Agno retries when a run yields nothing usable; agent instructions
  that ask for companies which actually employ people; and — the part that is a
  guarantee rather than a request — a gate in Go that refuses to store a company
  whose careers page cannot be resolved. Purged the 26 already stored.
- Added Darwinbox, common across Indian employers. Its board is a POST search
  endpoint that answers a bare client with a bot-check page, so the request
  carries the headers a browser sends from the careers page; that keeps it on
  the free path instead of costing a rendered scrape. Verified against five
  tenants: 24, 77 and 54 roles, one genuinely empty board, and one 403 no header
  set clears (the check is on the TLS fingerprint).
- Repaired PROGRESS.md. Bytes 684–10038 were UTF-16LE inside an otherwise UTF-8
  file — 2,364 interleaved NULs, which is why git had been treating it as binary.
- Configured Playwright MCP server (`@playwright/mcp`) in `.agents/mcp_config.json` and `.mcp.json` with `-y` auto-confirm flag to prevent stdio process hanging.

## 2026-08-27
- Restricted GitHub repository fetch limit to 3 repos max inside Go backend service.
- Implemented `/auth/me` endpoint in Go and global `AuthContext` in React to fix session loss on browser refresh.
- Added `ProtectedRoute` in React to prevent unauthorized access to dashboard routes without login.
- Added IP-based rate limiting (5 req/sec) to the Go backend API routes for abuse prevention.
- Generated a formal Production Readiness Checklist for future deployment planning.

- Performed full-repo ponytail audit to reduce bloat
- Cleaned up redundant auth checks in backend Go controllers
- Compressed repetitive React useEffect fetch promise chains using optional chaining
- Removed dead mock link code from InterviewSession frontend


- Transitioned hardcoded URLs to Environment Variables (VITE_API_URL and FRONTEND_URL)
- Hardened cookie security with HttpOnly, Secure (in production), and SameSite=Lax flags
- Verified Python API is protected by INTERNAL_SECRET dynamic check, preventing unauthorized DeepSeek quota usage

‭潃普杩牵摥䜠䵌㔠㈮洠摯汥椠⁮潬慣⁬灏湥潃敤攠瑸湥楳湯猠瑥楴杮⁳楶⁡噎䑉䅉丠䵉䄠䥐朠瑡睥祡മਊⴀ 倀漀渀礀琀愀椀氀 愀甀搀椀琀㨀 䐀爀漀瀀瀀攀搀 㠀 甀渀甀猀攀搀 琀愀戀氀攀猀 昀爀漀洀 匀甀瀀愀戀愀猀攀 ⠀瀀椀渀渀攀搀开爀攀瀀漀猀Ⰰ 焀甀攀猀琀椀漀渀猀开戀愀渀欀Ⰰ 猀攀猀猀椀漀渀开焀甀攀猀琀椀漀渀猀Ⰰ 挀愀渀搀椀搀愀琀攀开爀攀瀀漀爀琀猀Ⰰ 椀渀琀攀爀瘀椀攀眀开甀猀愀最攀Ⰰ 愀渀愀氀礀稀攀开甀猀愀最攀Ⰰ 椀渀琀攀爀瘀椀攀眀开爀攀挀漀爀搀椀渀最猀Ⰰ 瀀爀漀挀琀漀爀椀渀最开攀瘀攀渀琀猀⤀ 琀漀 欀攀攀瀀 琀栀攀 搀愀琀愀戀愀猀攀 氀攀愀渀 愀渀搀 猀琀爀椀挀琀氀礀 愀氀椀最渀攀搀 眀椀琀栀 琀栀攀 䜀漀 䜀伀刀䴀 洀漀搀攀氀猀⸀਀ഀ਀਀ⴀ 䤀洀瀀氀攀洀攀渀琀攀搀 渀愀琀椀瘀攀 䜀漀 最漀爀漀甀琀椀渀攀 昀漀爀 愀猀礀渀挀 䄀䤀 爀攀瀀漀 愀渀愀氀礀猀椀猀 ⠀愀瘀漀椀搀猀 䠀吀吀倀 琀椀洀攀漀甀琀猀⤀ 愀渀搀 愀搀搀攀搀 刀攀愀挀琀 昀爀漀渀琀攀渀搀 瀀漀氀氀椀渀最 攀渀搀瀀漀椀渀琀 ⠀⼀愀瀀椀⼀爀攀瀀漀猀⼀愀渀愀氀礀稀攀⼀猀琀愀琀甀猀⤀ 瀀攀爀 搀漀挀 　㘀਀ⴀ 䤀洀瀀氀攀洀攀渀琀攀搀 栀愀爀搀 戀椀氀氀椀渀最 氀椀洀椀琀猀㨀 唀猀攀爀猀 挀愀渀 漀渀氀礀 愀渀愀氀礀稀攀 愀 洀愀砀椀洀甀洀 漀昀 ㌀ 甀渀椀焀甀攀 爀攀瀀漀猀椀琀漀爀椀攀猀 ⠀䐀䈀 爀漀眀 氀椀洀椀琀猀⤀ 琀漀 瀀爀攀瘀攀渀琀 䰀䰀䴀 焀甀漀琀愀 愀戀甀猀攀਀ഀ਀਀ⴀ 䤀洀瀀氀攀洀攀渀琀攀搀 䌀愀洀攀爀愀 吀椀攀爀 䄀 ⠀瀀甀爀攀 昀爀漀渀琀攀渀搀 猀攀氀昀ⴀ瘀椀攀眀 瀀爀攀瘀椀攀眀 椀渀 䤀渀琀攀爀瘀椀攀眀匀攀猀猀椀漀渀⤀ 瀀攀爀 搀漀挀 　㘀⸀ 倀漀渀礀琀愀椀氀 愀瀀瀀爀漀愀挀栀㨀 稀攀爀漀 戀愀挀欀攀渀搀 爀攀挀漀爀搀椀渀最Ⰰ 稀攀爀漀 䄀䤀 愀渀愀氀礀猀椀猀Ⰰ 樀甀猀琀 猀椀洀瀀氀攀 渀愀瘀椀最愀琀漀爀⸀洀攀搀椀愀䐀攀瘀椀挀攀猀 琀漀 最椀瘀攀 琀栀攀 ✀瀀爀漀昀攀猀猀椀漀渀愀氀 瀀爀漀挀琀漀爀椀渀最✀ 昀攀攀氀 眀椀琀栀漀甀琀 琀栀攀 挀漀猀琀 漀爀 挀漀洀瀀氀攀砀椀琀礀⸀਀ഀ਀਀ⴀ 䤀洀瀀氀攀洀攀渀琀攀搀 ✀倀漀渀礀琀愀椀氀✀ 嘀漀椀挀攀 䤀渀琀攀爀瘀椀攀眀 匀礀猀琀攀洀㨀 䄀䤀 猀瀀攀愀欀猀 焀甀攀猀琀椀漀渀猀 瘀椀愀 圀攀戀 匀瀀攀攀挀栀 䄀倀䤀 ⠀猀瀀攀攀挀栀匀礀渀琀栀攀猀椀猀⤀ 愀渀搀 挀愀渀搀椀搀愀琀攀 愀渀猀眀攀爀猀 瘀椀愀 圀攀戀 匀瀀攀攀挀栀 䄀倀䤀 ⠀匀瀀攀攀挀栀刀攀挀漀最渀椀琀椀漀渀⤀⸀ 娀攀爀漀 戀愀挀欀攀渀搀 挀漀猀琀Ⰰ 稀攀爀漀 圀攀戀匀漀挀欀攀琀 挀漀洀瀀氀攀砀椀琀礀Ⰰ 椀渀猀琀愀渀琀 氀愀琀攀渀挀礀⸀਀ഀ਀ⴀ 䌀漀渀瘀攀爀琀攀搀 䜀椀琀䠀甀戀 氀漀最椀渀 愀渀挀栀漀爀 琀愀最 琀漀 愀 戀甀琀琀漀渀 眀椀琀栀 攀砀瀀氀椀挀椀琀 䨀匀 渀愀瘀椀最愀琀椀漀渀 琀漀 昀椀砀 椀渀琀攀爀洀椀琀琀攀渀琀 挀氀椀挀欀 戀氀漀挀欀椀渀最 椀渀 猀漀洀攀 戀爀漀眀猀攀爀猀⸀ഀ਀਀⌣㈠㈰ⴶ㠰㈭‷儨⁁楦數⥳ⴊ䘠硩摥映潲瑮湥⽤攮癮›瑩眠獡猠癡摥愠⁳呕ⵆ㘱‬潳嘠呉彅偁彉剕⁌慷⁳楳敬瑮祬甠摮晥湩摥愠摮攠敶祲愠瑵敨瑮捩瑡摥䄠䥐挠污⁬慷⁳楨瑴湩⁧桴⁥牷湯⁧剕⹌删ⵥ慳敶⁤獡瀠慬湩唠䙔㠭‮刨潯⁴慣獵⁥景洠獯⁴畡桴搯獡扨慯摲椯瑮牥楶睥戠敲歡条⁥潦湵⁤湩儠⁁慰獳⤮ⴊ删灥牯⹴獴⁸潮⁷獵獥嘠呉彅偁彉剕⁌湩瑳慥⁤景愠栠牡捤摯摥氠捯污潨瑳㠺㠰ⰰ洠瑡档湩⁧桴⁥敲瑳漠⁦桴⁥灡⹰ⴊ䜠慵摲摥漠敶慲汬獟潣敲⼠爠灥彯畦汬湟浡⁥条楡獮⁴畮汬椠⁮敒潰瑲琮硳‬慄桳潢牡⹤獴ⱸ删灥牯獴楌瑳琮硳琠⁯牰癥湥⁴牣獡敨⁳湯椠据浯汰瑥⁥敲潰瑲搠瑡⹡ⴊ䐠獩扡敬⁤桴⁥潮⵮畦据楴湯污∠潃瑮湩敵眠瑩⁨潇杯敬•畢瑴湯眠瑩⁨湡栠湯獥⁴匢潯≮猠慴整椠獮整摡漠⁦⁡楳敬瑮搠慥⁤汣捩⹫ⴊ匠摩扥牡琯灯慢⁲潮⁷桳睯琠敨爠慥⁬楳湧摥椭⁮楇䡴扵椠敤瑮瑩⁹愨慶慴Ⱳ甠敳湲浡ⱥ攠慭汩 湩瑳慥⁤景愠栠牡捤摯摥∠慃摮摩瑡≥瀠慬散潨摬牥ਮ‭楗敲⁤灵琠敨爠灥⁯湡⁤敲潰瑲猠慥捲⁨潢數⁳挨楬湥⵴楳敤映汩整⥲※楤慳汢摥琠敨琠灯慢⁲敳牡档戯汥⽬敨灬椠潣獮眠瑩⁨潴汯楴獰甠瑮汩琠敨⁹牡⁥敲污ਮ‭敒潭敶⁤桴⁥慦敫∠ㄫ┲瘠⁳慬瑳眠敥≫猠慴⁴湡⁤敧敮楲⁣牧敥楴杮漠⁮桴⁥慄桳潢牡㭤朠敲瑥湩⁧潮⁷獵獥琠敨爠慥⁬楇䡴扵甠敳湲浡⹥ⴊ删湥浡摥琠敨⼠湩整癲敩⽷猺獥楳湯摉爠畯整瀠牡浡琠⁯椯瑮牥楶睥㨯敲潰摉琠⁯慭捴⁨桷瑡椠⁴捡畴污祬栠汯獤ਮ‭灁汰敩⁤⁡潣獮獩整瑮搠獥杩⁮祳瑳浥愠牣獯⁳桴⁥桷汯⁥牦湯整摮⠠䉉⁍汐硥匠湡⽳慓獮䌠湯敤獮摥䴯湯Ɐ挠潯⵬敮瑵慲⁬慰敬瑴ⱥ栠楡汲湩⁥潢摲牥⥳瘠慩吠楡睬湩⁤桴浥⁥潴敫獮椠⁮湩敤⹸獣⁳ⴭ琠楨⁳污潳映硩摥愠猠牴祡搠牡⁫慢⁲瑡琠敨戠瑯潴⁭景琠敨洠扯汩⁥慬摮湩⁧慰敧‬桷捩⁨慷⁳潢祤扻ⵧ牧祡㤭〰⁽汢敥楤杮瀠獡⁴桴⁥楬桧⁴慰敧挠湯整瑮ਮ‭敖楲楦摥眠瑩⁨瑠捳ⴠ恢⠠汣慥⥮愠摮怠硯楬瑮⁠漨汮⁹牰ⵥ硥獩楴杮眠牡楮杮⥳愠瑦牥琠敨挠慨杮獥ਮ⌊‣〲㘲〭ⴸ㜲⠠畆汬攠摮琭ⵯ湥⁤䅑瀠獡⁳‫楦數⥳ⴊ䐠慩湧獯摥愠摮映硩摥琠敨爠慥⁬潬楧⵮潮⵴瑳捩楫杮戠杵›⽠畡桴洯恥甠敳⁤佇䵒猧怠楆獲⡴甦敳Ⱳ甠敳䥲⥄⁠楷桴愠唠䥕ⵄ瑳楲杮瀠楲慭祲欠祥‬桷捩⁨佇䵒倯獯杴敲⁳業⵳慰獲獥愠⁳⁡慲⁷兓⁌潣摮瑩潩⁮湩瑳慥⁤景愠瘠污敵⠠䕠剒剏›牴楡楬杮樠湵⁫晡整⁲畮敭楲⁣楬整慲恬⸩䌠慨杮摥琠⁯坠敨敲∨摩㴠㼠Ⱒ甠敳䥲⥄䘮物瑳☨獵牥怩‮楇䡴扵氠杯湩渠睯愠瑣慵汬⁹数獲獩獴ਮ‭楆數⁤⁡敲污瀠牯⁴潣汬獩潩⁮湯氠捯污潨瑳㠺〰‰敢睴敥⁮⁡潄正牥穩摥䜠楬捴周灩椠獮慴据⁥湡⁤桴⁥楡眭牯敫⁲䘨獡䅴䥐 ⴭ爠灥⁯湡污獹獩眠獡猠汩湥汴⁹楨瑴湩⁧汇瑩档楔⁰湡⁤慦汩湩⹧䴠癯摥愠⵩潷歲牥琠⁯潰瑲㠠〰‱湡⁤灵慤整⁤奐䡔乏坟剏䕋归剕⹌ⴊ愠⵩潷歲牥渠睯渠敥獤怠ⴭ湥⵶楦敬⸠⼮攮癮⁠桷湥猠慴瑲摥洠湡慵汬⁹椨⁴慨⁳潮搠瑯湥⁶潬摡牥漠⁦瑩⁳睯⥮漠⁲䕄偅䕓䭅䅟䥐䭟奅椠⁳浥瑰⁹湡⁤湡污獹獩映楡獬ਮ‭慂正湥⁤敒潰猠牴捵⁴慷⁳業獳湩⁧摠獥牣灩楴湯⽠池湡畧条恥映潲⁭桴⁥楇䡴扵䄠䥐爠獥潰獮⁥ⴭ删灥獯瑩牯敩⁳慰敧愠睬祡⁳桳睯摥∠潎搠獥牣灩楴湯•湡⁤⁡敧敮楲⁣慬杮慵敧戠摡敧攠敶⁮桴畯桧䜠瑩畈⁢慨⁤桴⁥慤慴ਮ‭楇䡴扵怠甯敳恲漠業獴攠慭汩眠敨⁮瑩猧猠瑥琠⁯牰癩瑡⁥挨浯潭⥮‮慃汬慢正渠睯映污獬戠捡⁫潴怠甯敳⽲浥楡獬⁠潦⁲桴⁥牰浩牡⁹敶楲楦摥愠摤敲獳‬湡⁤慢正楦汬⁳瑩映牯攠楸瑳湩⁧獵牥⁳潴⹯ⴊ怠癯牥污彬潣灭敬楸祴⁠牦浯琠敨䄠⁉潷歲牥眠獡愠映汵⁬敳瑮湥散‬潮⁴⁡桳牯⁴慬敢⁬ⴭ戠潲敫琠敨䄠慮祬楳⁳潃灭敬整挠牡❤⁳慬潹瑵‮捓敨慭渠睯愠歳⁳潦⁲⁡湯ⵥ潷摲氠扡汥瀠畬⁳⁡敳慰慲整怠潣灭敬楸祴牟慥潳楮杮⁠楦汥㭤映潲瑮湥⁤污潳搠来慲敤⁳牧捡晥汵祬映牯漠摬挠捡敨⁤湡污獹獥ਮ‭杠瑩畨形牰景汩獥攮灸物獥慟恴眠獡渠癥牥猠瑥漠⁮湩敳瑲⠠瑳捵⁫瑡稠牥ⵯ慶畬⥥‮潎⁷敳⁴潴䄠慮祬敺䅤⁴‫‷慤獹ਮ‭⽠灡⽩湩整癲敩獷焯敵瑳潩獮⁠敲畴湲⁳筠畱獥楴湯㩳嬠⸮崮恽‬畢⁴桴⁥牦湯整摮挠敨正摥怠牁慲⹹獩牁慲⡹慤慴怩漠⁮桴⁥桷汯⁥慰汹慯⁤ⴭ愠睬祡⁳慦獬ⱥ猠⁯桴⁥湩整癲敩⁷捳敲湥猠異⁮湯∠敇敮慲楴杮琠楡潬敲⁤畱獥楴湯⹳⸮•潦敲敶⁲楷桴渠⁯牥潲⹲䘠硩摥琠⁯敲摡怠慤慴焮敵瑳潩獮Ⱡ愠摮渠睯猠潨獷愠⁮牥潲⁲湩瑳慥⁤景栠湡楧杮椠⁦桴⁥桳灡⁥獩攠敶⁲牷湯⁧条楡⹮ⴊ怠湩整癲敩彷敳獳潩獮椮瑮牥楶睥瑟灹恥愠摮怠洮摯恥愠敲丠呏丠䱕⁌潣畬湭⁳牦浯愠⁮汯敤⁲捳敨慭琠慨⁴桴⁥畣牲湥⁴潇洠摯汥渠癥牥瀠灯汵瑡摥ⴠ‭癥牥⁹湩整癲敩⁷畳浢獩楳湯映楡敬⁤楷桴愠倠獯杴敲⁳潣獮牴楡瑮攠牲牯愠瑦牥愠爠慥ⱬ猠捵散獳畦⁬䥁攠慶畬瑡潩⹮䄠摤摥戠瑯⁨楦汥獤琠⁯桴⁥潭敤⽬敲畱獥⁴湡⁤楷敲⁤潭敤⠠整瑸瘯楯散 桴潲杵⁨牦浯琠敨映潲瑮湥⹤ⴊ䐠獡扨慯摲愠摮删灥牯獴氠獩⁴牴慥整⁤潠敶慲汬獟潣敲⁠〨ㄭ‰捳污ⱥ瀠牥删灥牯⹴獴❸⁳砢砮ㄯ∰ 獡椠⁦瑩眠牥⁥污敲摡⁹⁡ⴰ〱‰数捲湥慴敧ⴠ‭癥牥⁹敲污猠潣敲搠獩汰祡摥ㄠ砰琠潯氠睯愠摮琠敨䔠䍘䱅䕌呎䄯䕖䅒䕇丯䕅卄删噅䕉⁗桴敲桳汯獤⠠〸㔯⤰渠癥牥洠瑡档摥爠慥⁬慤慴‬潳朠潯⁤湩整癲敩獷愠睬祡⁳桳睯摥丠䕅卄删噅䕉⹗䘠硩摥琠牨獥潨摬⁳潴㠠㔯愠摮洠汵楴汰⁹祢ㄠ‰潦⁲桴⁥楤灳慬敹⁤数捲湥慴敧ਮ‭敖楲楦摥琠敨映汵⁬畡桴湥楴慣整⁤汦睯氠癩⁥湥ⵤ潴攭摮眠瑩⁨⁡敲污䜠瑩畈⁢潬楧㩮删灥獯瑩牯敩⁳㸭䄠慮祬敺ⴠ‾敔瑸䤠瑮牥楶睥⠠祴数⁤‫歳灩数⁤湡睳牥⥳ⴠ‾畓浢瑩ⴠ‾敒潰瑲‬污⁬敲摮牥湩⁧潣牲捥汴⁹楷桴爠慥⁬䥁札湥牥瑡摥挠湯整瑮ਮ‭敒潭敶⁤桴⁥整灭牯牡⁹⽠畡桴弯摟扥杵獟瑥獟獥楳湯⁠潲瑵⁥獵摥漠汮⁹潦⁲楤条潮楳杮琠敨挠潯楫⁥獩畳⁥ⴭ挠湯楦浲摥朠湯⁥㐨㐰 敢潦敲映湩獩楨杮ਮ- Decision: Deferred Razorpay billing integration (Step 13) to Phase 2, focusing purely on core functionality first.
- Redesigned InterviewSession UI to feature a side-by-side 'Google Meet' style layout integrating the camera preview prominently alongside the AI question visualizer.
- Updated AI Worker schema (AnalysisResult) to capture 3-5 line 'code_snippets' and 'file_references', enabling context-aware and deeply technical interview questions.
- Updated Architecture docs to emphasize production-grade scaling decisions (in-memory processing, Go concurrency, ETags, decoupled Postgres).


## 2026-08-28 (Job Map feature + security/QA/UI audit)

> Full detail, including handoff notes for the next agent, is in **SESSION_2026-08-28_JOB_MAP.md**.

- Built the **Job Map** (/directory) - an automatic startup + real-jobs directory, replacing the disabled Job Map sidebar placeholder. A Go cron (@every 6h + one run at startup) rotates 24 seed queries through an Agno discovery_agent (DeepSeek + DuckDuckGo + structured output), upserts companies deduped by domain, geocodes them via free Nominatim/OSM, and exposes them on a public /api/companies. Frontend has a grid + Leaflet map toggle with clustering and filters. No data is scraped from bangalorestartupmap.com - it was inspiration only.
- **Real job listings, not just careers links**: services.DetectATS finds a company Greenhouse/Lever board with pure HTTP (regex the careers page for an embedded board link, else verify a slug guess against the API) - deliberately NOT via the LLM, which is unreliable and costs tokens per company. SyncJobsForCompany then pulls live roles from the official public Greenhouse/Lever JSON APIs, dedupes on (company_id, url), and drops closed postings. Verified live: Razorpay -> 20 real roles, re-sync idempotent (no dupes); Zerodha correctly detected as having no ATS.
- Gave the two **candidate-facing** Agno agents (questions_agent, evaluation_agent) hiring-manager instructions so they sound like a real interviewer; left the two internal agents alone to avoid paying tokens for persona nobody reads. Verified live with a generated question that referenced the candidate actual adapter-pattern code.
- **Security/correctness audit - 10 findings, all fixed**: OAuth CSRF state was a hardcoded literal (zero protection) -> now crypto/rand + session-bound, one-time use; INTERNAL_SECRET fell back to a source-visible default -> now fails closed; OAuth redirect hardcoded to localhost -> now FRONTEND_URL; background goroutine had no recover() (one bad repo could crash the process for every user) -> added; camera kept streaming after toggle-off -> fixed the async race; PYTHON_WORKER_URL fallback still pointed at the dead port 8000 in 4 files -> 8001; **TOCTOU race let users exceed the 3-repo limit** -> now a per-user Postgres advisory lock, verified with a 6-way concurrency test (exactly 3 succeeded); global rate limiter -> per-IP; status endpoint reported DB errors as processing (frontend polled forever) -> now distinguishes them; getDefaultBranch ignored HTTP status -> now checks it.
- **Bonus bug caught while testing** (not in the audit): the new pending-placeholder row wrote an empty string into analysis_json, a jsonb column that Postgres rejects - EVERY analyze request would have failed. Fixed to "null".
- **UI/UX pass** (desktop + 390px mobile, live and logged in): removed a duplicate NeuroFIQ logo in the mobile sidebar; added the missing dimmed backdrop + tap-to-close; fixed the landing page Voice mode card overflowing on mobile; hid the empty Strengths block on skipped questions in the report; relabeled the misleading Dashboard Connected Repos stat to Repos Interviewed; fixed Job Map logos silently falling back to a generic globe (Google favicon endpoint returns a 16x16 placeholder that still decodes, so onError never fired -> now also checks naturalWidth); fixed the map showing only the first page of companies and not fitting the viewport to its pins.
- Verified throughout with go build ./..., go vet ./..., tsc -b, and live in-browser testing against the real logged-in app.

## 2026-08-29 (Job Map: 4 more ATS platforms)

- Added **SmartRecruiters, Ashby, Workable and Keka** to the ATS job-sync pipeline (Greenhouse + Lever were already there). Same pattern: regex the careers page HTML for an embedded board link, else guess the slug and verify against the provider API. No new deps, no DB change, no frontend change.
- **Keka finding**: their official developer API is partner-gated, but every Keka-hosted careers portal exposes `<slug>.keka.com/careers/api/jobs/default/active` publicly with no auth - found it by watching the network tab on a real Keka careers page. The read path has to be public because visitors are not logged in; the gated API is for HR admins writing data.
- **Bug found and fixed**: the Greenhouse regex missed regional boards. Groww uses `job-boards.eu.greenhouse.io`, which the old `(?:boards|job-boards)\.greenhouse\.io` pattern did not match. Now allows an optional region segment.
- **Bug found and fixed**: the periodic sync only queried companies where `ats_type != ''`, so any company whose detection failed once was never re-checked. That meant adding new ATS providers would have had zero effect on existing rows - only newly-discovered companies would benefit. Renamed to `SyncAllCompanyJobs` and it now re-detects companies with no ATS on every tick, and logs a summary line.
- Added a validity filter: rows missing title or url are dropped before upsert, so a provider shape change cannot write junk rows.
- **Verified end-to-end against live boards** (throwaway companies, cleaned up after): Swiggy 75 (SmartRecruiters), Freshworks 100 (SmartRecruiters), Ramp 138 (Ashby), Meesho 48 (Lever), Groww 5 (Greenhouse), plus a Keka board parsing multi-location correctly. Re-sync on each was idempotent - no duplicate rows.
- **Verified on the real directory**: after restart, re-detection found 3 companies that previously showed no jobs. Jupiter -> 12 real roles via Keka, Turtlemint and Upstox via SmartRecruiters. Confirmed in the browser UI with departments and locations rendering.
- **Not verified**: Workable. Its widget endpoint responds 200 and returns a well-formed empty array, and the v3 POST endpoint returns '{"total":0,"results":[]}' - but none of ~20 sampled slugs had active postings, so the parse path has never seen real data. Implemented per the documented shape; treat as unproven until a live Workable board with jobs is found.
- PhonePe now returns 404 from Greenhouse (previously had a board). Correctly detected as no-ATS - not a bug.

## 2026-08-29 (Workday + Firecrawl/Jina + credit guards)

> Full handoff doc: **SESSION_JOB_MAP_HANDOFF.md** - read that first.

- Added **Workday** support (7th ATS). Slug stored as `tenant:region:site`; the site id isn't in the URL so detection probes the common ones. Verified: BrowserStack -> 32 real roles, idempotent re-sync.
- Added `services/scrape_service.go` - **Firecrawl primary, Jina Reader fallback**, both hosted so we never run headless Chrome ourselves. Auto-switches to Jina on budget-exceeded or any Firecrawl error. Usage tracked per month+provider in a new `scrape_usages` table and logged each sync.
- `DetectATS` is now **three tiers, cheapest first**: plain HTTP -> hosted render (costs a credit) -> slug guess. Tier 2 only runs when tier 1 finds nothing.
-   D e c i s i o n :   D e f e r r e d   R a z o r p a y   b i l l i n g   i n t e g r a t i o n   ( S t e p   1 3 )   t o   P h a s e   2 ,   f o c u s i n g   p u r e l y   o n   c o r e   f u n c t i o n a l i t y   f i r s t .  
 -   R e d e s i g n e d   I n t e r v i e w S e s s i o n   U I   t o   f e a t u r e   a   s i d e - b y - s i d e   ' G o o g l e   M e e t '   s t y l e   l a y o u t   i n t e g r a t i n g   t h e   c a m e r a   p r e v i e w   p r o m i n e n t l y   a l o n g s i d e   t h e   A I   q u e s t i o n   v i s u a l i z e r .  
 -   U p d a t e d   A I   W o r k e r   s c h e m a   ( A n a l y s i s R e s u l t )   t o   c a p t u r e   3 - 5   l i n e   ' c o d e _ s n i p p e t s '   a n d   ' f i l e _ r e f e r e n c e s ' ,   e n a b l i n g   c o n t e x t - a w a r e   a n d   d e e p l y   t e c h n i c a l   i n t e r v i e w   q u e s t i o n s .  
 -   U p d a t e d   A r c h i t e c t u r e   d o c s   t o   e m p h a s i z e   p r o d u c t i o n - g r a d e   s c a l i n g   d e c i s i o n s   ( i n - m e m o r y   p r o c e s s i n g ,   G o   c o n c u r r e n c y ,   E T a g s ,   d e c o u p l e d   P o s t g r e s ) .  
 

## 2026-08-28 (Job Map feature + security/QA/UI audit)

> Full detail, including handoff notes for the next agent, is in **SESSION_2026-08-28_JOB_MAP.md**.

- Built the **Job Map** (/directory) - an automatic startup + real-jobs directory, replacing the disabled Job Map sidebar placeholder. A Go cron (@every 6h + one run at startup) rotates 24 seed queries through an Agno discovery_agent (DeepSeek + DuckDuckGo + structured output), upserts companies deduped by domain, geocodes them via free Nominatim/OSM, and exposes them on a public /api/companies. Frontend has a grid + Leaflet map toggle with clustering and filters. No data is scraped from bangalorestartupmap.com - it was inspiration only.
- **Real job listings, not just careers links**: services.DetectATS finds a company Greenhouse/Lever board with pure HTTP (regex the careers page for an embedded board link, else verify a slug guess against the API) - deliberately NOT via the LLM, which is unreliable and costs tokens per company. SyncJobsForCompany then pulls live roles from the official public Greenhouse/Lever JSON APIs, dedupes on (company_id, url), and drops closed postings. Verified live: Razorpay -> 20 real roles, re-sync idempotent (no dupes); Zerodha correctly detected as having no ATS.
- Gave the two **candidate-facing** Agno agents (questions_agent, evaluation_agent) hiring-manager instructions so they sound like a real interviewer; left the two internal agents alone to avoid paying tokens for persona nobody reads. Verified live with a generated question that referenced the candidate actual adapter-pattern code.
- **Security/correctness audit - 10 findings, all fixed**: OAuth CSRF state was a hardcoded literal (zero protection) -> now crypto/rand + session-bound, one-time use; INTERNAL_SECRET fell back to a source-visible default -> now fails closed; OAuth redirect hardcoded to localhost -> now FRONTEND_URL; background goroutine had no recover() (one bad repo could crash the process for every user) -> added; camera kept streaming after toggle-off -> fixed the async race; PYTHON_WORKER_URL fallback still pointed at the dead port 8000 in 4 files -> 8001; **TOCTOU race let users exceed the 3-repo limit** -> now a per-user Postgres advisory lock, verified with a 6-way concurrency test (exactly 3 succeeded); global rate limiter -> per-IP; status endpoint reported DB errors as processing (frontend polled forever) -> now distinguishes them; getDefaultBranch ignored HTTP status -> now checks it.
- **Bonus bug caught while testing** (not in the audit): the new pending-placeholder row wrote an empty string into analysis_json, a jsonb column that Postgres rejects - EVERY analyze request would have failed. Fixed to "null".
- **UI/UX pass** (desktop + 390px mobile, live and logged in): removed a duplicate NeuroFIQ logo in the mobile sidebar; added the missing dimmed backdrop + tap-to-close; fixed the landing page Voice mode card overflowing on mobile; hid the empty Strengths block on skipped questions in the report; relabeled the misleading Dashboard Connected Repos stat to Repos Interviewed; fixed Job Map logos silently falling back to a generic globe (Google favicon endpoint returns a 16x16 placeholder that still decodes, so onError never fired -> now also checks naturalWidth); fixed the map showing only the first page of companies and not fitting the viewport to its pins.
- Verified throughout with go build ./..., go vet ./..., tsc -b, and live in-browser testing against the real logged-in app.

## 2026-08-29 (Job Map: 4 more ATS platforms)

- Added **SmartRecruiters, Ashby, Workable and Keka** to the ATS job-sync pipeline (Greenhouse + Lever were already there). Same pattern: regex the careers page HTML for an embedded board link, else guess the slug and verify against the provider API. No new deps, no DB change, no frontend change.
- **Keka finding**: their official developer API is partner-gated, but every Keka-hosted careers portal exposes `<slug>.keka.com/careers/api/jobs/default/active` publicly with no auth - found it by watching the network tab on a real Keka careers page. The read path has to be public because visitors are not logged in; the gated API is for HR admins writing data.
- **Bug found and fixed**: the Greenhouse regex missed regional boards. Groww uses `job-boards.eu.greenhouse.io`, which the old `(?:boards|job-boards)\.greenhouse\.io` pattern did not match. Now allows an optional region segment.
- **Bug found and fixed**: the periodic sync only queried companies where `ats_type != ''`, so any company whose detection failed once was never re-checked. That meant adding new ATS providers would have had zero effect on existing rows - only newly-discovered companies would benefit. Renamed to `SyncAllCompanyJobs` and it now re-detects companies with no ATS on every tick, and logs a summary line.
- Added a validity filter: rows missing title or url are dropped before upsert, so a provider shape change cannot write junk rows.
- **Verified end-to-end against live boards** (throwaway companies, cleaned up after): Swiggy 75 (SmartRecruiters), Freshworks 100 (SmartRecruiters), Ramp 138 (Ashby), Meesho 48 (Lever), Groww 5 (Greenhouse), plus a Keka board parsing multi-location correctly. Re-sync on each was idempotent - no duplicate rows.
- **Verified on the real directory**: after restart, re-detection found 3 companies that previously showed no jobs. Jupiter -> 12 real roles via Keka, Turtlemint and Upstox via SmartRecruiters. Confirmed in the browser UI with departments and locations rendering.
- **Not verified**: Workable. Its widget endpoint responds 200 and returns a well-formed empty array, and the v3 POST endpoint returns '{"total":0,"results":[]}' - but none of ~20 sampled slugs had active postings, so the parse path has never seen real data. Implemented per the documented shape; treat as unproven until a live Workable board with jobs is found.
- PhonePe now returns 404 from Greenhouse (previously had a board). Correctly detected as no-ATS - not a bug.

## 2026-08-29 (Workday + Firecrawl/Jina + credit guards)

> Full handoff doc: **SESSION_JOB_MAP_HANDOFF.md** - read that first.

- Added **Workday** support (7th ATS). Slug stored as `tenant:region:site`; the site id isn't in the URL so detection probes the common ones. Verified: BrowserStack -> 32 real roles, idempotent re-sync.
- Added `services/scrape_service.go` - **Firecrawl primary, Jina Reader fallback**, both hosted so we never run headless Chrome ourselves. Auto-switches to Jina on budget-exceeded or any Firecrawl error. Usage tracked per month+provider in a new `scrape_usages` table and logged each sync.
- `DetectATS` is now **three tiers, cheapest first**: plain HTTP -> hosted render (costs a credit) -> slug guess. Tier 2 only runs when tier 1 finds nothing.
- **Credit guard**: added `ats_checked_at` + a 7-day recheck interval. Without it every 6h tick re-scraped all ~23 ATS-less companies = ~2,400 credits/month against a 1,000 free tier. One real run had already burned 20.
- **Bug: discovery failure blocked job sync entirely.** `RunDiscoveryRotation` returned early on error so `SyncAllCompanyJobs` never ran - one flaky web search meant zero job refresh for the whole tick. The two halves are independent now.
- **Bug: unchecked `Find()` error.** A failed query produced an empty slice and logged '0 companies checked' as if normal. Now reports the error and distinguishes it from a genuinely empty directory.
- **Bug: no timeout on the ai-worker call.** A stuck worker held the startup sync for ~3 hours (22:16 -> 01:09 in the logs). Now a 3-minute client timeout.
- **Bug: ai-worker crash** - 'str' object has no attribute 'model_dump'. With tools enabled Agno doesn't always return a parsed model; raw JSON and markdown-fenced JSON are both normalised now.
- **Result**: 35 -> 54 open roles. Zypp Electric contributed 19 via a Keka board that **only Firecrawl found** - plain HTTP missed it. Last sync: '27 companies checked, 1 newly detected, 54 open roles'.
- Honest note: Firecrawl mattered less than expected for detection - BrowserStack's Workday link was already visible to plain HTTP. The bigger win was adding Workday itself; Firecrawl mainly revealed which platform was missing.
- **Still open (highest priority): Nominatim has no rate limit.** Their policy is 1 req/sec and we call it once per new company with no throttle - real IP-ban risk.
- **Ponytail cleanup**: Removed 78.4 MB of compiled binaries (`backend-go.exe`, `tmp_server`), scratch files, dev logs, and unused frontend boilerplate assets. Created a comprehensive root `.gitignore` to prevent binaries, secrets, and build logs from being tracked in git.

## 2026-08-29 (later: dedupe, hiring-only filter, careers-URL resolver)

> Full handoff doc: **SESSION_JOB_MAP_HANDOFF.md** - read that first.

- **Tier 4 added - careers-page job extraction.** Companies with no supported ATS (the majority) now get their jobs pulled straight off their own careers page via Firecrawl LLM extraction, tagged `source: careers-page`. This was the real gap: 10 of 16 hiring companies use a custom portal. Verified on Doceree (19 real roles), Schoolnet (16), BYJU'S Exam Prep (15).
- **Tier 0 added - `ResolveCareersURL`.** The agent often omits the careers URL or points it at the homepage, leaving the company permanently at zero jobs. Now probes /careers, /jobs, /careers/jobs etc on the company domain and verifies the page contains careers vocabulary. Free (plain HTTP). Recovered 4 companies.
- **Duplicate companies merged.** Domain-only dedupe let BYJU'S through twice (byjus.com and byjusexamprep.com). Added `normalizeCompanyName` - strips parentheticals, legal suffixes, punctuation - so "BYJU'S Exam Prep (Gradeup)" and "BYJU'S Exam Prep" collapse to one key. Merged 2 existing dupes, keeping the row with more jobs.
- **"Hiring only" toggle, default ON.** Checked bangalorestartupmap directly: they show 1,045 companies but have a ?hiring=1 filter reading "991 open roles across 95 companies" (9% hiring). Ours now reads "196 open roles across 16 companies" (31%). A directory full of empty cards is useless, so browsing everything is opt-out.
- Companies now sort most-jobs-first. API returns `open_roles` alongside `total`.
- **Nominatim rate limit added** - mutex-based 1.1s throttle. Their policy is 1 req/sec and we had none; this was the biggest outstanding production risk.
- **Compound-area geocoding fallback.** "Noida/Gurugram, Delhi NCR" resolved to nothing, so Zypp Electric had no map pin despite having 19 jobs. Now falls back to simpler forms.
- **Researched and rejected** (documented in the handoff doc so nobody redoes it): webclaw (self-hosted version doesn't render JS), Lightpanda (3MB RAM but missed the link Firecrawl found - tested in Docker), Crawl4AI (Playwright underneath = same RAM), TheirStack (200 jobs/month free), Google Maps (no careers URL, needs a card).
- **Confirmed the reference site uses the same method we do**: ClickPost and Ctruh -> Keka, Bureau -> Ashby. Same ATS public APIs. No Naukri/Wellfound/Cutshort. The gap is scale, not method.
- **Result: 27 companies / 54 roles -> 51 companies / 196 roles** over the day.
- **Next up (priority order)**: swap DuckDuckGo for Serper (weakest link - it's why ~13 companies arrived with no careers URL), job field/level facets, retry failed extractions, follow "View Openings" links.

## 2026-08-29 (final: Exa search + job facets)

> Full handoff doc: **SESSION_JOB_MAP_HANDOFF.md**

- **Swapped DuckDuckGo for Exa** as the discovery agent's primary search (`ExaTools(category="company")`). DuckDuckGo was returning blog posts and listicles *about* companies rather than the companies themselves - the reason ~13 companies had no usable careers URL. Measured on one query: 5 of 6 companies came back with a careers URL, and the companies were real funded businesses (Perfios, Plum, Jodo). First full run added **10 new companies in one cycle** vs 1-3 before.
- DuckDuckGo is deliberately kept as a keyless fallback, so discovery degrades rather than stopping if the Exa key is missing or its credits run out.
- Agent also got explicit instructions: careers URL is the highest-value field, return the company's own domain (never an aggregator or news article), skip anything unverifiable.
- **Job facets added** (`services/job_facets.go`) - FIELD and LEVEL chips with live counts, matching the reference site. Derived from the title/department already stored: pure Go, no extra data, no LLM call. Clicking a chip filters the roles inside each company card and map popup.
  - Bucket order matters: Data & AI is checked before Engineering, else "Data Engineer" lands in the wrong bucket.
  - "Unspecified" is a real bucket (108 of 213), not a fallback bug - most titles say nothing about seniority, and guessing would be wrong more often than admitting we don't know.
- **Deliberately did NOT add Tavily/Apify yet.** Both researched and viable (Apify's compass/crawler-google-places gives Google Maps data without a Google Cloud card), but adding three search providers at once makes it impossible to tell which one helped.
- **Result: 51 companies / 196 roles -> 67 companies / 213 roles.** Hiring ratio 28% vs the reference site's 9%.
- Next up: retry the extractions that returned 0 (Classplus, Physics Wallah), follow "View Openings" links on marketing-style careers pages, and a direct Exa lookup for careers URLs from Go (a lookup, not a judgment call - skips LLM tokens).

## 2026-08-29 (production hardening + shareable reports, recruiter side, commit-history questions)

Merged the open `fix/user-reliability-and-repo-choice` work and then took the
whole codebase over the line for launch traffic. Verified with `go build ./...`,
`go vet ./...`, `go test -race ./...`, `tsc -b`, `oxlint` and `vite build`.

### The one that mattered most: the extractor was blind to most languages

`processZip` scored only `.go/.py/.ts/.js` by suffix. **`.tsx` does not end in
`.ts` and `.jsx` does not end in `.js`** — so a React + TypeScript repository
contributed *zero* source files and the interview was generated from
`package.json` alone. Java, Rust, Ruby, C#, Kotlin, Swift, PHP and C++ scored 0
and were dropped entirely. The product's whole claim — "we read your actual
code" — was silently failing for the majority of GitHub.

- Replaced the suffix checks with a 50-extension language table, plus manifest
  and entrypoint tables and a bonus for files under a directory that names a
  layer (`services/`, `controllers/`, `internal/`…).
- **`break` → `continue` in the budget loop.** Files are sorted by score, so one
  70k-char `main.go` sorted first, blew the 60k budget, and exited the loop with
  *zero* snippets. Now oversized files are truncated to 8k (the head carries the
  imports and entry points) and the budget fills with 8–10 files instead of 1–2.
- Real language detection replaces the literal placeholder string
  `"Auto-detected from files"` we were paying prompt tokens for.
- Directory tree is now grouped by directory with file counts, instead of a flat
  list truncated mid-path inside the first alphabetical folder.
- Binary files, lockfiles, `node_modules/`, `vendor/`, `dist/` and generated
  files are excluded before they reach the prompt.
- Regression tests cover all of it, including the `.tsx` case that started this.

### Fixes to the merged PR

- **The question cache was deleted, not replaced.** The lookup was removed but
  the write kept, so the cache was written and never read: every interview page
  load — including a refresh, and React StrictMode's dev double-mount — was a
  fresh DeepSeek call and five more rows in `questions_bank`. Restored the read
  path, keyed on a fingerprint of the analysis JSON so re-analysing a repo
  correctly invalidates its questions.
- **The retry path bypassed the free-tier limit.** The `failed → pending` branch
  committed without counting existing rows, and the count query excludes failed
  ones — so three good analyses plus one failure could be retried into a fourth
  live slot, and every further failure raised the ceiling again. The count now
  runs before both branches. Same TOCTOU class the advisory lock was added for;
  the lock was still there, the check just wasn't.
- **The SSRF guard was written but never called.** `allowedPublicURL` and
  friends had no callers anywhere in the branch while `fetchText` still fetched
  LLM-supplied URLs on `http.DefaultClient`. Now wired — see below.
- Restored ~300 lines of stripped comments (the advisory-lock rationale, the
  goroutine `recover()` note, why Exa beat DuckDuckGo, why the internal secret
  fails closed) and every explanatory line in `.env.example`.
- `_ = config.DB.Save(&user).Error` was silently discarding a failed write; logs
  again. `extractor_service.go` was left on `http.DefaultClient` while the other
  two worker callers were migrated. `/auth/logout` shipped with no caller —
  there is now a Sign out button.

### Security

- **SSRF, properly.** All third-party fetching goes through one client whose
  dialer `Control` hook rejects loopback, private, link-local, CGNAT and
  cloud-metadata addresses. The check runs *after* DNS resolution, on the
  address actually being dialled, so it also defeats DNS rebinding and a 302 to
  `169.254.169.254` — which a URL-string check does not. Every URL we fetch
  comes from an LLM's web search, so this is not theoretical.
- **ATS slugs are validated.** A slug scraped by regex was interpolated straight
  into `https://<slug>.keka.com/…`; `evil.example.com/x?` would have sent the
  request somewhere else entirely.
- **Session cookies are now encrypted, not just signed.** The cookie carries the
  user's GitHub OAuth token; signing proves we issued it but leaves the contents
  readable by anyone holding it.
- **Gin trusted no proxies before.** It trusted *all* of them, so any client
  could set `X-Forwarded-For` and choose its own `ClientIP` — the exact key the
  rate limiter uses. Now configured via `TRUSTED_PROXIES`, defaulting to trusting
  nothing.
- **`/api/interviews/submit` was an open LLM proxy.** No cap on `qa_list` length
  or answer size, and no check that the questions were ones we issued. Now
  bounded at 5 questions / 6k chars, validated against `questions_bank`, gated on
  the user owning a completed analysis, and capped at 20 interviews/day.
- The zipball download is bounded (120 MB) and so is decompression (200 MB): the
  old unbounded `io.ReadAll` was an out-of-memory kill switch any user could pull
  by pointing us at a large monorepo, or a zip bomb.

### Reliability under real traffic

- **Timeouts everywhere.** All seven ATS fetchers, the careers-page fetch and
  the GitHub calls were on `http.DefaultClient`, which has **no timeout**.
  PROGRESS.md already records this costing a ~3-hour stall once; it was fixed
  for the worker call only. Nothing uses the default client now.
- **Graceful shutdown.** A deploy used to kill the process mid-request, losing
  interview evaluations we had already paid the LLM for.
- **Stale analyses are reclaimed.** A crash or deploy left rows stuck on
  `pending` forever — a spinner that never resolves and a free slot the user
  could not get back. Swept at boot and every 15 minutes.
- **The rate limiter leaked memory.** One bucket per IP, never evicted, so any
  attacker forging source addresses could grow the map without bound. Same for
  the repo ETag cache, which held up to 100 repo records per user who ever
  logged in. Both are now swept and capped.
- **The job sync is bounded and single-instance.** It looped every company
  serially with several network calls each: fine at 67 companies, not at a few
  thousand, where the tick outruns the hour and cron starts the next one on top.
  Now a pool of 8. And because the cron lives in the API process, two containers
  meant two discovery runs an hour — double the LLM spend and every board
  scraped twice. A `cron_leases` table with an expiring lease picks one.
- DB pool limits (GORM's default is unlimited, which the Supabase pooler refuses
  before Go stops asking), server read/write timeouts, a 1 MB request body cap,
  and a per-user limiter on the endpoints that actually cost money — the per-IP
  one never covered a single account grinding away under 5 req/s.

### Three features, in the order they matter

1. **Public shareable reports.** A finished report was the only artifact this
   product creates that anyone wants to send to somebody, and it died behind the
   login. `POST /api/reports/:id/share` mints an unguessable slug; `/r/:slug`
   renders score, assessment and a "generated from this repository" mark to
   anyone. The candidate's raw answers are deliberately withheld. Revoking
   clears the slug so the old link 404s immediately.
2. **The recruiter side.** Nobody pays to be interviewed; companies pay for
   signal. Recruiters mint invite links (`/invite/:token`), candidates redeem
   them by taking the *same* interview on their own repo, and the recruiter gets
   them back ranked by score with the full report. Redemption is a single
   conditional UPDATE, so two candidates racing a single-use link cannot both
   get in.
3. **Commit-history questions.** We downloaded the whole zipball and threw the
   history away — `CommitStats` was hardcoded to `{1, 1}`. Two extra GitHub calls
   now give real commit and contributor counts plus the substantive commits, with
   merge/bump/typo noise filtered and recurring subjects ranked first because a
   repeated subject line is where somebody changed their mind. The analysis agent
   turns those into `history_observations` and the question agent spends exactly
   one of the five on them. No other product can ask "you rewrote this three
   times in one week — what did the first two get wrong?"

Plus: each question now carries the `file_reference` and `code_snippet` it was
built from, and the interview UI shows that code beside the question — the data
was already in the payload and the UI was throwing it away.

### Frontend

- **The webcam turned on during text interviews.** `CameraPreview` was rendered
  unconditionally; only the voice visualiser was gated. Someone who chose "Start
  Text Interview" got a camera permission prompt anyway. Now voice-only, and the
  text pane shows the code under discussion instead.
- The interview page swallowed the server's error text (`.then(res => res.json())`
  with no `res.ok` check), so the new, useful 400s — "analysis is still running",
  "retry analyzing this repository" — all rendered as one generic line.
- StrictMode double-mount guard on the question fetch: in dev it was billing two
  LLM calls per page load.
- Sign out, a recruiter nav group that only hiring accounts see, and the GitHub
  mark extracted into one shared component instead of being redefined per page.

### CI

There was no `.github/` directory and not one test in the repository. Added a
workflow running gofmt/build/vet/`go test -race`, a byte-compile of the worker
(whose errors otherwise only surface as a failed analysis in production), and
`npm ci` + lint + `tsc -b` + build. Go tests cover the extractor's language
coverage and budget packing, repo-name and ATS-slug validation, the SSRF IP
predicate, invite redemption, and the job facet buckets.

 # #   2 0 2 6 - 0 9 - 0 2 
 -   M o v e d   ' C l e a n   D e a d   J o b s '   l o g i c   f r o m   a   f r o n t e n d - t r i g g e r e d   A P I   r o u t e   t o   a n   a u t o m a t i c   b a c k e n d   c r o n   j o b   r u n n i n g   e v e r y   1 2   h o u r s .  
 -  
 I m p l e m e n t e d  
 G o  
 b a c k e n d  
 b r i d g e  
 / a p i / r a d a r / a n a l y z e  
 t o  
 p r o x y  
 r e q u e s t s  
 t o  
 P y t h o n  
 a i - w o r k e r  
 -  
 C o n n e c t e d  
 R e a c t  
 R a d a r . t s x  
 t o  
 r e a l  
 b a c k e n d  
 / a p i / r a d a r / a n a l y z e  
 a n d  
 m a p p e d  
 d a t a  
 -  
 C o m p l e t e l y  
 r e d e s i g n e d  
 R a d a r . t s x  
 w i t h  
 p r e m i u m  
 g l a s s m o r p h i c  
 U I  
 a n i m a t e d  
 S V G  
 p r o g r e s s  
 r i n g s  
 a n d  
 d y n a m i c  
 s k i l l  
 p i l l s .  
 -  
 P i v o t e d  
 R a d a r  
 f e a t u r e  
 f r o m  
 J o b  
 M a t c h e r  
 t o  
 P r o f i l e  
 O p t i m i z e r  
 a c r o s s  
 P y t h o n  
 A I  
 W o r k e r  
 G o  
 B a c k e n d  
 a n d  
 R e a c t  
 F r o n t e n d  
 p e r  
 u s e r  
 r e q u e s t .  
 -  
 U p g r a d e d  
 P r o f i l e  
 O p t i m i z e r  
 U I  
 t o  
 a  
 V e r c e l / L i n e a r  
 i n s p i r e d  
 D a r k  
 M o d e  
 B e n t o  
 B o x  
 d e s i g n  
 w i t h  
 t e r m i n a l  
 s c a n n i n g  
 a n i m a t i o n s .  
 -  
 A d d e d  
 L i n k e d I n  
 L o g i n  
 W a l l  
 d e t e c t i o n  
 i n  
 A I  
 w o r k e r  
 s c r a p e r  
 t o  
 r e t u r n  
 g r a c e f u l  
 e r r o r  
 i n  
 U I  
 i n s t e a d  
 o f  
 a  
 g e n e r i c  
 c r a s h .  
 