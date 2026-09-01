from fastapi import FastAPI, Header, HTTPException, Depends
from pydantic import BaseModel, Field
from typing import List
import json
import os

from agno.agent import Agent
from agno.models.deepseek import DeepSeek
from agno.tools.exa import ExaTools
from agno.exceptions import CheckTrigger, OutputCheckError
from agno.run.agent import RunOutput

app = FastAPI()

INTERNAL_SECRET = os.getenv("INTERNAL_SECRET")
DEEPSEEK_API_KEY = os.getenv("DEEPSEEK_API_KEY")

# ---- Pydantic models for the incoming payload ----
class CodeSnippet(BaseModel):
    file: str
    line_range: str
    content: str

class StructureSummary(BaseModel):
    directory_tree: str
    languages: List[str]

class NotableCommit(BaseModel):
    message: str = ""
    date: str = ""
    author: str = ""

class CommitStats(BaseModel):
    total_commits: int = 0
    contributors: int = 0
    # Optional so an older Go build, or a repo whose history we could not
    # read, still posts a payload this worker accepts.
    first_commit_at: str = ""
    last_commit_at: str = ""
    notable_commits: List[NotableCommit] = []

class AnalyzePayload(BaseModel):
    repo_full_name: str
    structure_summary: StructureSummary
    code_snippets: List[CodeSnippet]
    commit_stats: CommitStats

class ProbingArea(BaseModel):
    topic: str = Field(description="The architectural concept or code issue to probe.")
    file_reference: str = Field(description="The specific file name where this was observed.")
    code_snippet: str = Field(description="A short 3-5 line code snippet showing the exact implementation to base the question on.")

# ---- Pydantic model for structured output (Agno) ----
class AnalysisResult(BaseModel):
    architecture_patterns: List[str]
    overall_complexity: str = Field(
        description="A single word only: Low, Moderate, or High. No explanation, no punctuation."
    )
    complexity_reasoning: str = Field(
        description="One short sentence explaining the complexity rating."
    )
    strengths: List[str]
    areas_for_probing: List[ProbingArea]
    # History-derived observations. Separate from areas_for_probing because
    # they come from the commit log rather than the code, and the question
    # agent is told to spend exactly one question on them.
    history_observations: List[str] = Field(
        default_factory=list,
        description="Up to 3 observations drawn ONLY from the commit history: things rewritten more than once, work that clustered in a short burst, or a decision the messages show being reversed. Empty list if the history says nothing interesting.",
    )

class QuestionItem(BaseModel):
    question_text: str
    expected_answer: str
    difficulty: str
    category: str
    # The snippet the question is about, so the UI can show the candidate the
    # code instead of making them go find it on GitHub.
    file_reference: str = Field(default="", description="The file this question is about, copied from the analysis. Empty for a history question.")
    code_snippet: str = Field(default="", description="The exact 3-5 line snippet this question refers to, copied verbatim from the analysis. Empty for a history question.")

class QuestionList(BaseModel):
    questions: List[QuestionItem]

class GenerateQuestionsPayload(BaseModel):
    repo_full_name: str
    analysis_data: str
    history_summary: str = ""

# ---- Pydantic models for evaluation ----
class QAItem(BaseModel):
    question: str
    answer: str

class EvaluatePayload(BaseModel):
    repo_full_name: str
    qa_list: List[QAItem]

class FeedbackItem(BaseModel):
    question: str
    score: float  # 0 to 10
    strengths: str
    areas_for_improvement: str
    ideal_answer_concept: str

class EvaluationResult(BaseModel):
    overall_score: float  # 0 to 10 weighted average
    overall_feedback: str
    detailed_feedback: List[FeedbackItem]

# ---- Pydantic models for company discovery ----
class DiscoveredCompany(BaseModel):
    name: str
    website: str = Field(description="Full https:// URL to the company's official homepage")
    description: str = Field(description="One sentence: what the company does")
    sector: str = Field(description="One of: AI, Fintech, SaaS, Healthtech, Edtech, D2C, Logistics, Deeptech, Consumer, Gaming, Other")
    stage: str = Field(description="One of: Bootstrapped, Pre-seed, Seed, Series A, Series B, Series C+, Public, Acquired, Unknown")
    area: str = Field(description="City or locality, e.g. 'Bangalore' or 'Koramangala, Bangalore'")
    careers_url: str = Field(description="URL to careers/jobs page if found, else empty string")

class CompanyDiscoveryResult(BaseModel):
    companies: List[DiscoveredCompany]

class ExtractedJob(BaseModel):
    title: str
    department: str = ""
    location: str = ""
    url: str = ""

class JobExtractionResult(BaseModel):
    jobs: List[ExtractedJob]

class ExtractJobsPayload(BaseModel):
    # The careers page as text, already fetched by Go. The worker never
    # fetches a URL itself — it stays a pure function of the text it is
    # handed, same as the analysis agent.
    page_text: str
    company_name: str = ""
    source_url: str = ""

class DiscoverCompaniesPayload(BaseModel):
    query: str
    limit: int = 10

def _discovery_tools():
    """Search tools for company discovery.

    Exa only. Its `category="company"` filter returns company homepages
    rather than blog posts and listicles about them, which is what a general
    web search kept returning and why so many companies arrived with no
    usable careers URL.

    DuckDuckGo used to sit here as a keyless fallback. It was removed: ddgs
    now raises DDGSException("No results found") on most queries, and an
    agent holding it would try it, fail, retry, and burn three to six
    minutes per query — well past the worker's timeout, so the tick returned
    nothing at all. A tool that always fails is not a fallback, it is a
    slower way to fail. Without an Exa key discovery has no search tool and
    the agent says so, which is the honest outcome.
    """
    if not os.getenv("EXA_API_KEY"):
        return []
    return [
        ExaTools(
            category="company",
            num_results=10,
            text_length_limit=2000,
            show_results=False,
        )
    ]


# ---- Initialise the Agno agents ----
# Every agent is declared as None first so that a missing API key produces a
# clean 500 from the route below rather than a NameError at import time.
analysis_agent = None
questions_agent = None
evaluation_agent = None

# Domains that are never the company itself. A discovery run that returns one
# of these has answered the wrong question: it found a page *about* the
# company instead of the company, and the careers-page resolver downstream
# will then probe a job board's own site rather than the employer's.
_AGGREGATOR_HOSTS = (
    "linkedin.com", "crunchbase.com", "tracxn.com", "indeed.com",
    "glassdoor.co", "naukri.com", "wellfound.com", "angel.co",
    "ambitionbox.com", "zaubacorp.com", "wikipedia.org", "youtube.com",
    "facebook.com", "instagram.com", "twitter.com", "x.com",
    "medium.com", "substack.com", "github.io", "notion.site",
)


def _host(url: str) -> str:
    from urllib.parse import urlparse
    try:
        return (urlparse(url).hostname or "").lower().lstrip("www.")
    except Exception:
        return ""


def drop_unusable_companies(run_output: RunOutput) -> None:
    """Filter a discovery run down to rows we can actually act on.

    This runs on the model's output rather than in the prompt because these
    are facts about the response, not judgements: a URL either is an
    aggregator or it is not. Prompt instructions are advisory and the model
    ignores them under load; this is not.

    What it deliberately does NOT try to decide is whether a company hires.
    A model cannot know that reliably, so guessing produces exactly the
    confident-but-wrong rows this exists to stop. That question is settled
    downstream by a free HTTP probe for a careers page, where the answer is
    a fact rather than an opinion.
    """
    result = getattr(run_output, "content", None)
    companies = getattr(result, "companies", None)
    if companies is None:
        return

    kept, seen = [], set()
    for c in companies:
        host = _host(c.website)
        if not host or "." not in host:
            continue
        if any(host == a or host.endswith("." + a) for a in _AGGREGATOR_HOSTS):
            print(f"discovery: dropped {c.name!r} — {host} is an aggregator, not the company")
            continue
        if not (c.name or "").strip():
            continue
        if host in seen:  # same company returned twice in one run
            continue
        seen.add(host)
        kept.append(c)

    # An empty result is a failed run, not a valid answer of "none exist".
    # Raising lets Agno's retry take another pass instead of writing nothing.
    if not kept:
        raise OutputCheckError(
            "no usable companies in this discovery run",
            check_trigger=CheckTrigger.VALIDATION_FAILED,
        )

    result.companies = kept


discovery_agent = None
extraction_agent = None

if DEEPSEEK_API_KEY:
    analysis_agent = Agent(
        model=DeepSeek(id="deepseek-chat", api_key=DEEPSEEK_API_KEY),
        output_schema=AnalysisResult,
        description="You are an expert Principal Software Engineer interviewing a candidate based on their Github repository.",
        instructions=[
            "Base every observation on the code and history you were given. Never invent a file, a pattern, or a commit.",
            "For history_observations, look for the same area being touched repeatedly, or a decision the messages show being reversed. If the history is thin, return an empty list rather than padding it.",
        ],
    )
    questions_agent = Agent(
        model=DeepSeek(id="deepseek-chat", api_key=DEEPSEEK_API_KEY),
        output_schema=QuestionList,
        description="You are an expert interviewer. Generate exactly 5 highly technical interview questions based on the candidate's repository analysis.",
        instructions=[
            "Phrase every question the way a real senior engineer would ask it out loud in a live interview — never as a numbered spec item.",
            "Reference the candidate's actual code/decisions by name so it feels like a conversation about their work, not a generic quiz.",
            "Sound curious and collegial, not like an exam — e.g. 'I noticed you used X here — walk me through why' rather than 'Explain your use of X.'",
            "For every question built on a code observation, copy the exact file_reference and code_snippet from the analysis into those fields. Copy them verbatim — do not paraphrase or reformat the code.",
            "If history_observations is non-empty, make exactly ONE of the five a history question: ask why something was reworked or reversed, citing the dates. Leave its file_reference and code_snippet empty.",
            "Never mention that you are an AI or refer to yourself in the third person.",
        ],
    )
    evaluation_agent = Agent(
        model=DeepSeek(id="deepseek-chat", api_key=DEEPSEEK_API_KEY),
        output_schema=EvaluationResult,
        description="You are an expert Principal Software Engineer evaluating a candidate's technical interview answers. Score them from 0-10 on correctness, depth of reasoning, communication clarity, and awareness of trade-offs.",
        instructions=[
            "Write feedback the way a thoughtful hiring manager would say it face-to-face — direct, specific, and encouraging even when the score is low.",
            "Acknowledge what the candidate got right before addressing gaps; never open with criticism.",
            "Avoid generic rubric language ('lacks depth', 'insufficient detail') — explain concretely what was missing and what a stronger answer would have covered.",
            "An answer of 'Skipped' or an empty answer scores 0 and the feedback should simply note it was not attempted.",
            "Never mention that you are an AI or that this is an automated evaluation.",
        ],
    )
    discovery_agent = Agent(
        model=DeepSeek(id="deepseek-chat", api_key=DEEPSEEK_API_KEY),
        tools=_discovery_tools(),
        output_schema=CompanyDiscoveryResult,
        post_hooks=[drop_unusable_companies],
        # A run that comes back with nothing usable is retried rather than
        # written through as an empty result. Two extra attempts, because a
        # third rarely differs and the search tools are the slow part.
        retries=2,
        description="You are a startup research analyst. Use web search to find REAL, currently operating companies matching the query. Only include companies you can verify have a real website. Never invent companies.",
        instructions=[
            "For every company, try hard to find its careers or jobs page URL — "
            "that is the single most valuable field. Look for links like "
            "/careers, /jobs, or 'work with us' on the company's own site.",
            "Return the company's own domain as the website, never an aggregator, "
            "directory listing, or news article about them.",
            "Skip anything you cannot verify has a real, live website.",
            # A search for "D2C startups in Delhi" returns small Shopify shops
            # next to funded companies, and they look identical in a result
            # list. They are the wrong answer here for a concrete reason: they
            # do not hire, so they enter the directory with no roles and stay
            # that way. Of the companies we stored that had no careers page at
            # all, 62% were D2C and 88% were pre-Series-A.
            "Only return companies that actually employ people and hire: "
            "funded (Series A or later), or an established profitable business "
            "with a real team. Skip single-founder shops, dropshipping and "
            "Shopify storefronts, and anything with no evidence of employees.",
            "If the only thing you can find is a storefront selling products — "
            "no team page, no careers page, no funding, no press — leave it out. "
            "Ten real hiring companies are worth more than fifty that never post "
            "a job.",
        ],
    )

    extraction_agent = Agent(
        model=DeepSeek(id="deepseek-chat", api_key=DEEPSEEK_API_KEY),
        output_schema=JobExtractionResult,
        description=(
            "You read the text of a company's careers page and list the job "
            "openings actually printed on it."
        ),
        instructions=[
            "Return only roles that appear on the page as openings. Never invent one, "
            "and never complete a partial list from what you know about the company.",
            "A careers page often carries a lot of text that is not a job: culture blurbs, "
            "benefits, employee quotes, office locations, blog links. Skip all of it.",
            "Some pages link to career *advice* articles — lists of professions like "
            "'Actor, Actuary, Aerospace Engineer'. That is not a list of openings at this "
            "company. If the list looks like an alphabetical index of professions, return "
            "an empty list.",
            "Copy each title as printed. Do not normalise 'SDE-2' into 'Software Engineer II'.",
            "Fill department and location only when the page states them for that role; "
            "leave them empty rather than guessing.",
            "For url, use the role's own apply link if the page gives one. If every role "
            "shares one generic apply button, leave url empty.",
            "If the page lists no openings, return an empty list. That is a correct answer.",
        ],
    )

# Handlers that call an Agno agent are plain `def`, not `async def`.
#
# agent.run() is synchronous and blocking. Inside an `async def` handler it
# blocks the event loop, so the worker serves exactly one agent call at a
# time however many arrive — four parallel discovery queries timed out at
# three minutes each waiting on one another. Declared `def`, FastAPI runs
# them in its threadpool and they genuinely overlap.

# ---- Dependencies ----
def verify_internal_secret(x_internal_secret: str = Header(None)):
    # Fail closed: if INTERNAL_SECRET isn't configured, reject every request
    # instead of falling back to a source-visible default.
    if not INTERNAL_SECRET or x_internal_secret != INTERNAL_SECRET:
        raise HTTPException(status_code=403, detail="forbidden")

def _history_block(stats: CommitStats) -> str:
    """Render the commit history for the analysis prompt.

    The repository archive shows what the code is now; the history is the only
    place that records what it used to be. That is where the questions no
    other product can ask come from.
    """
    if not stats.total_commits and not stats.notable_commits:
        return ""
    lines = [
        "\nCommit history:",
        f"{stats.total_commits} commits by {stats.contributors} contributor(s).",
    ]
    if stats.first_commit_at and stats.last_commit_at:
        lines.append(f"Active from {stats.first_commit_at} to {stats.last_commit_at}.")
    if stats.notable_commits:
        # collectCommitStats ranks by how often a subject recurs, not by date —
        # the repeatedly-revisited work is the interesting part. Label it
        # accurately so the agent does not make a claim about ordering.
        lines.append("Recent substantive commits (most-revisited work first):")
        for c in stats.notable_commits:
            lines.append(f"- {c.date}: {c.message}")
    return "\n".join(lines) + "\n"

# ---- Routes ----
@app.get("/internal/health")
async def health_check():
    return {"status": "healthy"}

@app.post("/internal/analyze-repo", dependencies=[Depends(verify_internal_secret)])
def analyze_repo(payload: AnalyzePayload):
    if not analysis_agent:
        raise HTTPException(status_code=500, detail="DEEPSEEK_API_KEY not configured for Agno Agent")

    # 1. Build the prompt
    prompt = (
        f"Repo: {payload.repo_full_name}\n\n"
        f"Languages present: {', '.join(payload.structure_summary.languages)}\n\n"
        f"Directory Structure:\n{payload.structure_summary.directory_tree}\n"
    )
    prompt += _history_block(payload.commit_stats)
    prompt += "\nCode Snippets:\n"
    for snippet in payload.code_snippets:
        prompt += f"\n--- {snippet.file} ({snippet.line_range}) ---\n{snippet.content}\n"

    prompt += "\nPlease analyze this codebase and extract the required architectural insights."

    # 2. Run the Agno agent
    try:
        # Agno forces the LLM to return data matching the response model, so
        # the Go side never has to strip markdown fences off a JSON blob.
        run_response = analysis_agent.run(prompt)

        # run_response.content is already a parsed AnalysisResult object.
        return run_response.content.model_dump()

    except Exception as e:
        print(f"Agno Agent Error: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/internal/generate-questions", dependencies=[Depends(verify_internal_secret)])
def generate_questions(payload: GenerateQuestionsPayload):
    if not questions_agent:
        raise HTTPException(status_code=500, detail="DEEPSEEK_API_KEY not configured for Agno Agent")

    prompt = (
        f"Repo: {payload.repo_full_name}\n\n"
        f"AI Analysis of the codebase (including key code snippets):\n{payload.analysis_data}\n\n"
    )
    if payload.history_summary:
        prompt += f"Commit history of the same repository:\n{payload.history_summary}\n\n"
    prompt += (
        "Based on the analysis, generate exactly 5 deep, technical interview questions. "
        "You MUST reference the specific 'code_snippet' and 'file_reference' from the analysis in your "
        "questions to make them highly contextual (e.g., 'In file.js, you wrote [snippet], why did you "
        "choose this approach over...'), and copy those two values into the question's own "
        "file_reference and code_snippet fields. Test the candidate's understanding of their own "
        "architecture, design decisions, and code quality."
    )

    try:
        run_response = questions_agent.run(prompt)
        # Return the list directly so Go can unmarshal into []models.Question
        return [q.model_dump() for q in run_response.content.questions]
    except Exception as e:
        print(f"Agno Agent Error (Questions): {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/internal/evaluate-answer", dependencies=[Depends(verify_internal_secret)])
def evaluate_answer(payload: EvaluatePayload):
    if not evaluation_agent:
        raise HTTPException(status_code=500, detail="DEEPSEEK_API_KEY not configured for Agno Agent")

    prompt = f"Repo context: {payload.repo_full_name}\n\nEvaluate the following Interview Questions and Answers:\n\n"
    for idx, qa in enumerate(payload.qa_list):
        prompt += f"Q{idx+1}: {qa.question}\nCandidate Answer: {qa.answer}\n\n"

    prompt += "Provide a detailed evaluation for each question and an overall score out of 10."

    try:
        run_response = evaluation_agent.run(prompt)
        return run_response.content.model_dump()
    except Exception as e:
        print(f"Agno Agent Error (Evaluation): {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/internal/discover-companies", dependencies=[Depends(verify_internal_secret)])
def discover_companies(payload: DiscoverCompaniesPayload):
    if not discovery_agent:
        raise HTTPException(status_code=500, detail="DEEPSEEK_API_KEY not configured for Agno Agent")

    prompt = f"Search the web and find up to {payload.limit} real companies matching: {payload.query}. For each, extract the required fields. Skip any company you can't find a real website for."

    try:
        run_response = discovery_agent.run(prompt)
        content = run_response.content

        # With tools enabled the agent doesn't always hand back a parsed
        # model — sometimes it's raw JSON text (or JSON wrapped in a
        # markdown fence). Normalise all three shapes here rather than
        # letting the caller 500.
        if hasattr(content, "model_dump"):
            return content.model_dump()

        if isinstance(content, dict):
            return CompanyDiscoveryResult(**content).model_dump()

        if isinstance(content, str):
            text = content.strip()
            if text.startswith("```"):
                text = text.split("```")[1]
                if text.lstrip().lower().startswith("json"):
                    text = text.lstrip()[4:]
            return CompanyDiscoveryResult(**json.loads(text)).model_dump()

        raise ValueError(f"unexpected discovery content type: {type(content).__name__}")
    except Exception as e:
        print(f"Agno Agent Error (Discovery): {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/internal/extract-jobs", dependencies=[Depends(verify_internal_secret)])
def extract_jobs(payload: ExtractJobsPayload):
    """Pull the open roles out of a careers page's text.

    Go fetches the page (free HTTP, then Jina, then Firecrawl only if those
    come up empty) and sends the text here. Keeping the fetch on the Go side
    and the reading on this side means the expensive rendered fetch and the
    LLM call are independent: a page that plain HTTP can read costs nothing
    to fetch, and this endpoint does not care which tier produced the text.
    """
    if not extraction_agent:
        raise HTTPException(status_code=500, detail="DEEPSEEK_API_KEY not configured for Agno Agent")

    # A careers page can carry a whole site's worth of markup. The openings
    # are near the top of the readable text, and sending 500KB would cost
    # tokens for navigation and footers.
    text = payload.page_text[:40000]
    if not text.strip():
        return JobExtractionResult(jobs=[]).model_dump()

    where = f" ({payload.source_url})" if payload.source_url else ""
    header = f"Careers page for {payload.company_name or 'a company'}{where}."
    prompt = f"""{header}

List the job openings on this page.

{text}"""

    try:
        run_response = extraction_agent.run(prompt)
        content = run_response.content
        # With a schema set Agno usually hands back the parsed model, but a
        # raw or fenced JSON string turns up often enough to normalise here
        # rather than 500 on it — same shapes the discovery endpoint sees.
        if hasattr(content, "model_dump"):
            return content.model_dump()
        if isinstance(content, dict):
            return JobExtractionResult(**content).model_dump()
        if isinstance(content, str):
            t = content.strip()
            if t.startswith("```"):
                t = t.split("```")[1]
                if t.lstrip().lower().startswith("json"):
                    t = t.lstrip()[4:]
            return JobExtractionResult(**json.loads(t)).model_dump()
        raise ValueError(f"unexpected extraction content type: {type(content).__name__}")
    except Exception as e:
        print(f"Agno Agent Error (Extraction): {e}")
        raise HTTPException(status_code=500, detail=str(e))
