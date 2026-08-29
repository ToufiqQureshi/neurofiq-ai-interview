from fastapi import FastAPI, Header, HTTPException, Depends
from pydantic import BaseModel, Field
from typing import List
import json
import os

from agno.agent import Agent
from agno.models.deepseek import DeepSeek
from agno.tools.duckduckgo import DuckDuckGoTools
from agno.tools.exa import ExaTools

app = FastAPI()

INTERNAL_SECRET = os.getenv("INTERNAL_SECRET")
DEEPSEEK_API_KEY = os.getenv("DEEPSEEK_API_KEY")

class CodeSnippet(BaseModel):
    file: str
    line_range: str
    content: str

class StructureSummary(BaseModel):
    directory_tree: str
    languages: List[str]

class CommitStats(BaseModel):
    total_commits: int
    contributors: int

class AnalyzePayload(BaseModel):
    repo_full_name: str
    structure_summary: StructureSummary
    code_snippets: List[CodeSnippet]
    commit_stats: CommitStats

class ProbingArea(BaseModel):
    topic: str = Field(description="The architectural concept or code issue to probe.")
    file_reference: str = Field(description="The specific file name where this was observed.")
    code_snippet: str = Field(description="A short 3-5 line code snippet showing the exact implementation to base the question on.")

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

class QuestionItem(BaseModel):
    question_text: str
    expected_answer: str
    difficulty: str
    category: str

class QuestionList(BaseModel):
    questions: List[QuestionItem]

class GenerateQuestionsPayload(BaseModel):
    repo_full_name: str
    analysis_data: str

class QAItem(BaseModel):
    question: str
    answer: str

class EvaluatePayload(BaseModel):
    repo_full_name: str
    qa_list: List[QAItem]

class FeedbackItem(BaseModel):
    question: str
    score: float
    strengths: str
    areas_for_improvement: str
    ideal_answer_concept: str

class EvaluationResult(BaseModel):
    overall_score: float
    overall_feedback: str
    detailed_feedback: List[FeedbackItem]

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

class DiscoverCompaniesPayload(BaseModel):
    query: str
    limit: int = 10

def _discovery_tools():
    tools = []
    if os.getenv("EXA_API_KEY"):
        tools.append(
            ExaTools(
                category="company",
                num_results=10,
                text_length_limit=2000,
                show_results=False,
            )
        )
    tools.append(DuckDuckGoTools())
    return tools

analysis_agent = None
questions_agent = None
evaluation_agent = None
discovery_agent = None
if DEEPSEEK_API_KEY:
    analysis_agent = Agent(
        model=DeepSeek(id="deepseek-chat", api_key=DEEPSEEK_API_KEY),
        output_schema=AnalysisResult,
        description="You are an expert Principal Software Engineer interviewing a candidate based on their Github repository.",
    )
    questions_agent = Agent(
        model=DeepSeek(id="deepseek-chat", api_key=DEEPSEEK_API_KEY),
        output_schema=QuestionList,
        description="You are an expert interviewer. Generate exactly 5 highly technical interview questions based on the candidate's repository analysis.",
        instructions=[
            "Phrase every question the way a real senior engineer would ask it out loud in a live interview — never as a numbered spec item.",
            "Reference the candidate's actual code/decisions by name so it feels like a conversation about their work, not a generic quiz.",
            "Sound curious and collegial, not like an exam — e.g. 'I noticed you used X here — walk me through why' rather than 'Explain your use of X.'",
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
            "Never mention that you are an AI or that this is an automated evaluation.",
        ],
    )
    discovery_agent = Agent(
        model=DeepSeek(id="deepseek-chat", api_key=DEEPSEEK_API_KEY),
        tools=_discovery_tools(),
        output_schema=CompanyDiscoveryResult,
        description="You are a startup research analyst. Use web search to find REAL, currently operating companies matching the query. Only include companies you can verify have a real website. Never invent companies.",
        instructions=[
            "For every company, try hard to find its careers or jobs page URL — "
            "that is the single most valuable field. Look for links like "
            "/careers, /jobs, or 'work with us' on the company's own site.",
            "Return the company's own domain as the website, never an aggregator, "
            "directory listing, or news article about them.",
            "Skip anything you cannot verify has a real, live website.",
        ],
    )

def verify_internal_secret(x_internal_secret: str = Header(None)):
    if not INTERNAL_SECRET or x_internal_secret != INTERNAL_SECRET:
        raise HTTPException(status_code=403, detail="forbidden")

@app.get("/internal/health")
async def health_check():
    return {"status": "healthy"}

@app.post("/internal/analyze-repo", dependencies=[Depends(verify_internal_secret)])
async def analyze_repo(payload: AnalyzePayload):
    if not analysis_agent:
        raise HTTPException(status_code=500, detail="DEEPSEEK_API_KEY not configured for Agno Agent")
    prompt = f"Repo: {payload.repo_full_name}\n\nDirectory Structure:\n{payload.structure_summary.directory_tree}\n\nCode Snippets:\n"
    for snippet in payload.code_snippets:
        prompt += f"\n--- {snippet.file} ---\n{snippet.content}\n"
    prompt += "\nPlease analyze this codebase and extract the required architectural insights."
    try:
        run_response = analysis_agent.run(prompt)
        return run_response.content.model_dump()
    except Exception as e:
        print(f"Agno Agent Error: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/internal/generate-questions", dependencies=[Depends(verify_internal_secret)])
async def generate_questions(payload: GenerateQuestionsPayload):
    if not questions_agent:
        raise HTTPException(status_code=500, detail="DEEPSEEK_API_KEY not configured for Agno Agent")
    prompt = f"Repo: {payload.repo_full_name}\n\nAI Analysis of the codebase (including key code snippets):\n{payload.analysis_data}\n\n"
    prompt += "Based on the analysis, generate exactly 5 deep, technical interview questions. You MUST reference the specific 'code_snippet' and 'file_reference' from the analysis in your questions to make them highly contextual (e.g., 'In file.js, you wrote [snippet], why did you choose this approach over...'). Test the candidate's understanding of their own architecture, design decisions, and code quality."
    try:
        run_response = questions_agent.run(prompt)
        return [q.model_dump() for q in run_response.content.questions]
    except Exception as e:
        print(f"Agno Agent Error (Questions): {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/internal/evaluate-answer", dependencies=[Depends(verify_internal_secret)])
async def evaluate_answer(payload: EvaluatePayload):
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
async def discover_companies(payload: DiscoverCompaniesPayload):
    if not discovery_agent:
        raise HTTPException(status_code=500, detail="DEEPSEEK_API_KEY not configured for Agno Agent")
    prompt = f"Search the web and find up to {payload.limit} real companies matching: {payload.query}. For each, extract the required fields. Skip any company you can't find a real website for."
    try:
        run_response = discovery_agent.run(prompt)
        content = run_response.content
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
