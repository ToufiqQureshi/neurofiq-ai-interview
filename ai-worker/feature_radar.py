import os
from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel, Field
from typing import List
from agno.agent import Agent
from agno.models.openrouter import OpenRouter
from deps import verify_internal_secret
from scraper import scrape_url

router = APIRouter()
OPEN_ROUTER_KEY = os.getenv("OPEN_ROUTER")

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

radar_agent = None

if OPEN_ROUTER_KEY:
    radar_agent = Agent(
        model=OpenRouter(id="deepseek/deepseek-chat", api_key=OPEN_ROUTER_KEY),
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

@router.post("/optimize-profile", dependencies=[Depends(verify_internal_secret)])
def optimize_profile(payload: ProfileRadarPayload):
    if not radar_agent:
        raise HTTPException(status_code=500, detail="OPEN_ROUTER key not configured")

    profile_text = scrape_url(payload.profile_url)
    if not profile_text:
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

    prompt = f"Analyze this candidate's profile:\n{profile_text}\n\n"
    prompt += "Provide optimization feedback to improve their ATS score and recruiter visibility."

    try:
        run_response = radar_agent.run(prompt)
        return run_response.content.model_dump()
    except Exception as e:
        print(f"Agno Agent Error (Profile Radar): {e}")
        raise HTTPException(status_code=500, detail=str(e))
