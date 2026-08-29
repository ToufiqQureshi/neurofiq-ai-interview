-- 002_companies.sql
-- "Job Map" company directory, populated automatically by the Agno discovery
-- agent (ai-worker /internal/discover-companies) via a Go cron rotation.
-- Note: at runtime the schema is actually provisioned by GORM AutoMigrate in
-- main.go; this file documents it for Supabase tooling and RLS.

CREATE TABLE companies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    description TEXT,
    website TEXT,
    domain TEXT NOT NULL UNIQUE,
    sector TEXT,
    stage TEXT,
    area TEXT,
    careers_url TEXT,
    lat DOUBLE PRECISION,
    lng DOUBLE PRECISION,
    source TEXT DEFAULT 'agno-discovery',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_companies_sector ON companies(sector);
CREATE INDEX idx_companies_stage ON companies(stage);
CREATE INDEX idx_companies_area ON companies(area);

ALTER TABLE companies ENABLE ROW LEVEL SECURITY;

-- Public directory: anyone can read.
CREATE POLICY "Anyone can view companies" ON companies FOR SELECT USING (true);
