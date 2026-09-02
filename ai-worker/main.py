from fastapi import FastAPI, Header, HTTPException, Depends
from pydantic import BaseModel, Field
from typing import List, Optional
import os

from agno.agent import Agent
from agno.models.deepseek import DeepSeek

from scraper import scrape_url

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
    # The Job Map opening this interview is practice for, as "<title> at
    # <company>". Optional, and resolved from the database by Go rather than
    # posted by a browser, so it names a role the pipeline actually found.
    target_role: str = ""

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

# ---- Pydantic models for Profile Radar ----
class ProfileRadarPayload(BaseModel):
    profile_url: str

class ProfileSectionFeedback(BaseModel):
    section: str = Field(description="e.g., 'Headline', 'Summary', 'Experience', 'Skills'")
    feedback: str = Field(description="Critique of the current section")
    suggestion: str = Field(description="Actionable suggestion or rewritten example")

class ProfileRadarResult(BaseModel):
    profile_name: str
    overall_score: int = Field(description="Profile strength score from 0 to 100")
    missing_keywords: List[str]
    section_feedbacks: List[ProfileSectionFeedback]
    general_advice: str

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
    radar_agent = Agent(
        model=DeepSeek(id="deepseek-chat", api_key=DEEPSEEK_API_KEY),
        output_schema=ProfileRadarResult,
        description="You are an expert tech recruiter and profile optimizer analyzing a candidate's LinkedIn, Wellfound, or GitHub profile.",
        instructions=[
            "Extract the candidate's name or username.",
            "Analyze the profile text for missing keywords, weak headline/summary, and poor bullet points.",
            "Generate actionable suggestions to improve visibility for recruiters and ATS.",
            "Provide a realistic overall profile strength score out of 100.",
            "Never mention that you are an AI.",
        ],
    )
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
async def analyze_repo(payload: AnalyzePayload):
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
async def generate_questions(payload: GenerateQuestionsPayload):
    if not questions_agent:
        raise HTTPException(status_code=500, detail="DEEPSEEK_API_KEY not configured for Agno Agent")

    prompt = (
        f"Repo: {payload.repo_full_name}\n\n"
        f"AI Analysis of the codebase (including key code snippets):\n{payload.analysis_data}\n\n"
    )
    if payload.history_summary:
        prompt += f"Commit history of the same repository:\n{payload.history_summary}\n\n"
    if payload.target_role:
        # Framing only. The questions must still come out of the candidate's
        # own code — that is the one thing we can ask about with authority,
        # and a role description is not evidence of anything they built. It
        # decides which parts of their work to press on, never the subject.
        prompt += (
            f"The candidate is preparing for this specific opening: {payload.target_role}\n"
            "Choose which parts of THEIR analysed codebase to press on so the questions "
            "rehearse what that role would be asked. Do not ask about the company, the "
            "job description, or any code the candidate did not write.\n\n"
        )
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

@app.post("/internal/optimize-profile", dependencies=[Depends(verify_internal_secret)])
async def optimize_profile(payload: ProfileRadarPayload):
    if not radar_agent:
        raise HTTPException(status_code=500, detail="DEEPSEEK_API_KEY not configured")

    # 1. Scrape the URL
    profile_text = scrape_url(payload.profile_url)
    if not profile_text:
        # Return a gracefully mocked result so the frontend can display the issue
        # instead of a 500/400 error which Go will mask into a generic failure.
        return {
            "profile_name": "Scraping Failed",
            "overall_score": 0,
            "missing_keywords": ["Accessible URL"],
            "section_feedbacks": [
                {
                    "section": "System",
                    "feedback": "We were unable to access this URL. The server timed out or blocked us.",
                    "suggestion": "Try providing a public Wellfound or GitHub profile instead, as some platforms heavily restrict automated scans."
                }
            ],
            "general_advice": "Failed to scrape this profile. We couldn't fetch the page content."
        }
        
    if profile_text == "ERROR_LOGIN_WALL_LINKEDIN":
        # Return a gracefully mocked result so the frontend can display the issue
        # instead of a 500/400 error which Go will mask into a generic failure.
        return {
            "profile_name": "Login Wall Detected",
            "overall_score": 0,
            "missing_keywords": ["Public Visibility"],
            "section_feedbacks": [
                {
                    "section": "Profile Privacy",
                    "feedback": "LinkedIn blocked our heuristic engine because your profile requires logging in to view.",
                    "suggestion": "Try providing a public Wellfound or GitHub profile instead, as LinkedIn aggressively blocks automated scans."
                }
            ],
            "general_advice": "We hit a login wall. LinkedIn aggressively blocks automated scrapers from viewing non-public profiles."
        }

    # 2. Build the LLM Prompt
    prompt = f"Analyze this candidate's profile:\n{profile_text}\n\n"
    prompt += "Provide optimization feedback to improve their ATS score and recruiter visibility."

    # 3. Call Agno Agent
    try:
        run_response = radar_agent.run(prompt)
        return run_response.content.model_dump()
    except Exception as e:
        print(f"Agno Agent Error (Profile Radar): {e}")
        raise HTTPException(status_code=500, detail=str(e))

