-- 001_initial_schema.sql

-- Enable pgcrypto for gen_random_uuid() if not already enabled
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ==========================================
-- TABLES
-- ==========================================

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    github_id BIGINT UNIQUE,
    github_username TEXT,
    google_id TEXT UNIQUE,
    email TEXT,
    avatar_url TEXT,
    plan_type TEXT DEFAULT 'free',
    role TEXT DEFAULT 'candidate',
    github_connected BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    last_login_at TIMESTAMPTZ
);

CREATE TABLE github_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    repo_full_name TEXT NOT NULL,
    repo_size_kb INTEGER,
    strategy_used TEXT,
    analysis_json JSONB,
    analyzed_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    UNIQUE (user_id, repo_full_name)
);

CREATE TABLE pinned_repos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    repo_full_name TEXT NOT NULL,
    pinned_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (user_id, repo_full_name)
);

CREATE TABLE questions_bank (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_full_name TEXT,
    source_github_profile_id UUID REFERENCES github_profiles(id) ON DELETE SET NULL,
    question_text TEXT NOT NULL,
    category TEXT NOT NULL,
    difficulty TEXT NOT NULL,
    code_reference JSONB,
    tech_stack TEXT[],
    reusable BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE interview_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    github_profile_id UUID REFERENCES github_profiles(id) ON DELETE SET NULL,
    interview_type TEXT NOT NULL,
    mode TEXT NOT NULL,
    camera_enabled BOOLEAN DEFAULT false,
    status TEXT DEFAULT 'in_progress',
    started_at TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE TABLE session_questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID REFERENCES interview_sessions(id) ON DELETE CASCADE,
    question_id UUID REFERENCES questions_bank(id) ON DELETE CASCADE,
    order_index INTEGER NOT NULL,
    candidate_answer TEXT,
    ai_feedback TEXT,
    score NUMERIC,
    follow_up_questions JSONB,
    answered_at TIMESTAMPTZ
);

CREATE TABLE candidate_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID REFERENCES interview_sessions(id) ON DELETE CASCADE UNIQUE,
    overall_score NUMERIC,
    strengths JSONB,
    weaknesses JSONB,
    improvement_suggestions JSONB,
    generated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE interview_usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    month_year TEXT NOT NULL,
    interview_count INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (user_id, month_year)
);

CREATE TABLE analyze_usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    count INTEGER DEFAULT 0,
    UNIQUE (user_id, date)
);

CREATE TABLE interview_recordings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID REFERENCES interview_sessions(id) ON DELETE CASCADE,
    video_url TEXT,
    duration_seconds INTEGER,
    file_size_mb NUMERIC,
    storage_status TEXT DEFAULT 'processing',
    consent_given BOOLEAN NOT NULL,
    retention_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE proctoring_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID REFERENCES interview_sessions(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    timestamp_in_session INTEGER,
    severity TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- ==========================================
-- INDEXES
-- ==========================================

CREATE INDEX idx_github_profiles_user_repo ON github_profiles(user_id, repo_full_name);
CREATE INDEX idx_sessions_user ON interview_sessions(user_id);
CREATE INDEX idx_usage_user_month ON interview_usage(user_id, month_year);
CREATE INDEX idx_analyze_usage_user_date ON analyze_usage(user_id, date);
CREATE INDEX idx_questions_reusable ON questions_bank(reusable) WHERE reusable = true;
CREATE INDEX idx_pinned_repos_user ON pinned_repos(user_id);
CREATE INDEX idx_proctoring_events_session ON proctoring_events(session_id);

-- ==========================================
-- ROW LEVEL SECURITY (RLS)
-- ==========================================

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE github_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE pinned_repos ENABLE ROW LEVEL SECURITY;
ALTER TABLE interview_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE session_questions ENABLE ROW LEVEL SECURITY;
ALTER TABLE candidate_reports ENABLE ROW LEVEL SECURITY;
ALTER TABLE interview_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE analyze_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE interview_recordings ENABLE ROW LEVEL SECURITY;
ALTER TABLE proctoring_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE questions_bank ENABLE ROW LEVEL SECURITY;

-- users
CREATE POLICY "Users can view own profile" ON users FOR SELECT USING (auth.uid() = id);
CREATE POLICY "Users can update own profile" ON users FOR UPDATE USING (auth.uid() = id);

-- github_profiles
CREATE POLICY "Users can view own github profiles" ON github_profiles FOR SELECT USING (auth.uid() = user_id);
CREATE POLICY "Users can insert own github profiles" ON github_profiles FOR INSERT WITH CHECK (auth.uid() = user_id);

-- pinned_repos
CREATE POLICY "Users can view own pinned repos" ON pinned_repos FOR SELECT USING (auth.uid() = user_id);
CREATE POLICY "Users can insert own pinned repos" ON pinned_repos FOR INSERT WITH CHECK (auth.uid() = user_id);
CREATE POLICY "Users can delete own pinned repos" ON pinned_repos FOR DELETE USING (auth.uid() = user_id);

-- interview_sessions
CREATE POLICY "Users can view own sessions" ON interview_sessions FOR SELECT USING (auth.uid() = user_id);
CREATE POLICY "Users can insert own sessions" ON interview_sessions FOR INSERT WITH CHECK (auth.uid() = user_id);
CREATE POLICY "Users can update own sessions" ON interview_sessions FOR UPDATE USING (auth.uid() = user_id);

-- session_questions
CREATE POLICY "Users can view own session questions" ON session_questions FOR SELECT USING (
    EXISTS (SELECT 1 FROM interview_sessions WHERE id = session_questions.session_id AND user_id = auth.uid())
);
CREATE POLICY "Users can insert own session questions" ON session_questions FOR INSERT WITH CHECK (
    EXISTS (SELECT 1 FROM interview_sessions WHERE id = session_id AND user_id = auth.uid())
);
CREATE POLICY "Users can update own session questions" ON session_questions FOR UPDATE USING (
    EXISTS (SELECT 1 FROM interview_sessions WHERE id = session_id AND user_id = auth.uid())
);

-- candidate_reports
CREATE POLICY "Users can view own candidate reports" ON candidate_reports FOR SELECT USING (
    EXISTS (SELECT 1 FROM interview_sessions WHERE id = candidate_reports.session_id AND user_id = auth.uid())
);
CREATE POLICY "Users can insert own candidate reports" ON candidate_reports FOR INSERT WITH CHECK (
    EXISTS (SELECT 1 FROM interview_sessions WHERE id = session_id AND user_id = auth.uid())
);

-- usage tracking
CREATE POLICY "Users can view own interview usage" ON interview_usage FOR SELECT USING (auth.uid() = user_id);
CREATE POLICY "Users can view own analyze usage" ON analyze_usage FOR SELECT USING (auth.uid() = user_id);

-- recordings
CREATE POLICY "Users can view own recordings" ON interview_recordings FOR SELECT USING (
    EXISTS (SELECT 1 FROM interview_sessions WHERE id = interview_recordings.session_id AND user_id = auth.uid())
);
CREATE POLICY "Users can delete own recordings" ON interview_recordings FOR DELETE USING (
    EXISTS (SELECT 1 FROM interview_sessions WHERE id = session_id AND user_id = auth.uid())
);

-- proctoring
CREATE POLICY "Users can view own proctoring events" ON proctoring_events FOR SELECT USING (
    EXISTS (SELECT 1 FROM interview_sessions WHERE id = proctoring_events.session_id AND user_id = auth.uid())
);

-- questions_bank (Shared read access for reusable, writes only via backend service role)
CREATE POLICY "Users can view reusable questions" ON questions_bank FOR SELECT USING (reusable = true);
