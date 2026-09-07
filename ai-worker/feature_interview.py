import os
from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel, Field
from typing import List
from agno.agent import Agent
from agno.models.openrouter import OpenRouter
from deps import verify_internal_secret

router = APIRouter()
OPEN_ROUTER_KEY = os.getenv("OPEN_ROUTER")

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
    history_observations: List[str] = Field(
        default_factory=list,
        description="Up to 3 observations drawn ONLY from the commit history: things rewritten more than once, work that clustered in a short burst, or a decision the messages show being reversed. Empty list if the history says nothing interesting.",
    )

class QuestionItem(BaseModel):
    question_text: str
    expected_answer: str
    difficulty: str
    category: str
    file_reference: str = Field(default="", description="The file this question is about, copied from the analysis. Empty for a history question.")
    code_snippet: str = Field(default="", description="The exact 3-5 line snippet this question refers to, copied verbatim from the analysis. Empty for a history question.")

class QuestionList(BaseModel):
    questions: List[QuestionItem]

class GenerateQuestionsPayload(BaseModel):
    repo_full_name: str
    analysis_data: str
    history_summary: str = ""
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

# ---- Agents ----
analysis_agent = None
questions_agent = None
evaluation_agent = None

if OPEN_ROUTER_KEY:
    analysis_agent = Agent(
        model=OpenRouter(id="deepseek/deepseek-chat", api_key=OPEN_ROUTER_KEY),
        output_schema=AnalysisResult,
        description="You are an expert Principal Software Engineer interviewing a candidate based on their Github repository.",
        instructions=[
            "Base every observation on the code and history you were given. Never invent a file, a pattern, or a commit.",
            "For history_observations, look for the same area being touched repeatedly, or a decision the messages show being reversed. If the history is thin, return an empty list rather than padding it.",
        ],
    )
    questions_agent = Agent(
        model=OpenRouter(id="deepseek/deepseek-chat", api_key=OPEN_ROUTER_KEY),
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
        model=OpenRouter(id="deepseek/deepseek-chat", api_key=OPEN_ROUTER_KEY),
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

def _history_block(stats: CommitStats) -> str:
    if not stats.total_commits and not stats.notable_commits:
        return ""
    lines = [
        "\nCommit history:",
        f"{stats.total_commits} commits by {stats.contributors} contributor(s).",
    ]
    if stats.first_commit_at and stats.last_commit_at:
        lines.append(f"Active from {stats.first_commit_at} to {stats.last_commit_at}.")
    if stats.notable_commits:
        lines.append("Recent substantive commits (most-revisited work first):")
        for c in stats.notable_commits:
            lines.append(f"- {c.date}: {c.message}")
    return "\n".join(lines) + "\n"

@router.post("/analyze-repo", dependencies=[Depends(verify_internal_secret)])
async def analyze_repo(payload: AnalyzePayload):
    if not analysis_agent:
        raise HTTPException(status_code=500, detail="OPEN_ROUTER key not configured for Agno Agent")

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

    try:
        run_response = analysis_agent.run(prompt)
        return run_response.content.model_dump()
    except Exception as e:
        print(f"Agno Agent Error: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@router.post("/generate-questions", dependencies=[Depends(verify_internal_secret)])
async def generate_questions(payload: GenerateQuestionsPayload):
    if not questions_agent:
        raise HTTPException(status_code=500, detail="OPEN_ROUTER key not configured for Agno Agent")

    prompt = (
        f"Repo: {payload.repo_full_name}\n\n"
        f"AI Analysis of the codebase:\n{payload.analysis_data}\n\n"
    )
    if payload.history_summary:
        prompt += f"Commit history:\n{payload.history_summary}\n\n"
    if payload.target_role:
        prompt += (
            f"The candidate is preparing for this specific opening: {payload.target_role}\n"
            "Choose which parts of THEIR analysed codebase to press on so the questions "
            "rehearse what that role would be asked. Do not ask about the company, the "
            "job description, or any code the candidate did not write.\n\n"
        )
    prompt += (
        "Based on the analysis, generate exactly 5 deep, technical interview questions. "
        "You MUST reference the specific 'code_snippet' and 'file_reference' from the analysis in your "
        "questions to make them highly contextual, and copy those two values into the question's own "
        "fields. Test the candidate's understanding of their own architecture, design decisions, and code quality."
    )

    try:
        run_response = questions_agent.run(prompt)
        return [q.model_dump() for q in run_response.content.questions]
    except Exception as e:
        print(f"Agno Agent Error (Questions): {e}")
        raise HTTPException(status_code=500, detail=str(e))

@router.post("/evaluate-answer", dependencies=[Depends(verify_internal_secret)])
async def evaluate_answer(payload: EvaluatePayload):
    if not evaluation_agent:
        raise HTTPException(status_code=500, detail="OPEN_ROUTER key not configured for Agno Agent")

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
