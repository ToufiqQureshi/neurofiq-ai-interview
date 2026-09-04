import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Briefcase,
  Layers,
  MapPin,
  Sparkles,
  Globe,
  CheckCircle2,
  RefreshCw,
  TrendingUp,
} from 'lucide-react';
import { UnifiedSearchCapsule } from '../components/UnifiedSearchCapsule';
import { PreferenceSentenceFilter } from '../components/PreferenceSentenceFilter';
import { JobListingCard, type JobCardData } from '../components/JobListingCard';
import { AiMatchSummaryCard } from '../components/AiMatchSummaryCard';
import CompanyDrawer from '../components/CompanyDrawer';

interface Company {
  id: string;
  name: string;
  description: string;
  website: string;
  domain: string;
  sector: string;
  stage: string;
  area: string;
  careers_url: string;
  lat: number | null;
  lng: number | null;
  job_count: number;
}

const POPULAR_CATEGORIES = [
  { label: 'All Tech Roles', value: '' },
  { label: 'Backend / Systems (Go/Java/Py)', value: 'Backend' },
  { label: 'Frontend / Fullstack (React/TS)', value: 'Frontend' },
  { label: 'AI / Machine Learning', value: 'AI' },
  { label: 'DevOps / Cloud Platform', value: 'DevOps' },
  { label: 'Mobile Engineering', value: 'Mobile' },
  { label: 'Data Analytics / Engineering', value: 'Data' },
  { label: 'Security & QA', value: 'Security' },
];

const SUB_HUBS = [
  { name: 'Pan-India', query: '' },
  { name: 'Bengaluru (HSR / Koramangala)', query: 'Bengaluru' },
  { name: 'Mumbai (BKC / Andheri / Powai)', query: 'Mumbai' },
  { name: 'Vasai-Virar (Palghar Suburbs)', query: 'Vasai' },
  { name: 'Delhi NCR (Cyber City / Noida)', query: 'Delhi' },
  { name: 'Hyderabad (Hitec City)', query: 'Hyderabad' },
  { name: 'Pune (Hinjawadi)', query: 'Pune' },
  { name: 'Remote Roles', query: 'Remote' },
];

export function JobsPortal() {
  const navigate = useNavigate();

  // Search & Filter State
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedLocation, setSelectedLocation] = useState('');
  const [selectedRole, setSelectedRole] = useState('Software Engineer');
  const [selectedExp, setSelectedExp] = useState('');
  const [selectedWorkType, setSelectedWorkType] = useState('');
  const [selectedCategory, setSelectedCategory] = useState('');

  // Data State
  const [companies, setCompanies] = useState<Company[]>([]);
  const [jobs, setJobs] = useState<JobCardData[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedCompany, setSelectedCompany] = useState<Company | null>(null);

  const handleSelectCompany = (companyId: string) => {
    const local = companies.find(c => c.id === companyId);
    if (local) {
      setSelectedCompany(local);
      return;
    }
    fetch(`${import.meta.env.VITE_API_URL}/api/companies/${companyId}`, {
      credentials: 'include',
    })
      .then(r => (r.ok ? r.json() : null))
      .then(c => {
        if (c) setSelectedCompany(c);
      })
      .catch(err => console.error('Failed to load company by id:', err));
  };

  const [totalLiveJobs, setTotalLiveJobs] = useState(3550);

  // Fetch Companies for Drawer & Global Directory Stats
  useEffect(() => {
    fetch(`${import.meta.env.VITE_API_URL}/api/companies?page_size=200`, {
      credentials: 'include',
    })
      .then(r => (r.ok ? r.json() : null))
      .then(d => {
        if (d?.companies) setCompanies(d.companies);
      })
      .catch(err => console.error('Failed to load companies:', err));
  }, []);

  // Fetch Real Live Jobs from /api/jobs with debounced search and filters
  useEffect(() => {
    let isMounted = true;
    setLoading(true);

    const debounceTimer = setTimeout(() => {
      const params = new URLSearchParams();
      params.set('page_size', '50');
      if (selectedLocation) params.set('location', selectedLocation);
      if (searchQuery) params.set('q', searchQuery);
      if (selectedCategory) params.set('field', selectedCategory);

      fetch(`${import.meta.env.VITE_API_URL}/api/jobs?${params.toString()}`, {
        credentials: 'include',
      })
        .then(r => (r.ok ? r.json() : null))
        .then(d => {
          if (!isMounted || !d) return;
          if (d.total) setTotalLiveJobs(d.total);
          const mappedJobs: JobCardData[] = (d.jobs || []).map((j: any) => ({
            id: j.id,
            title: j.title,
            companyName: j.company_name,
            companyDomain: j.company_domain,
            companyId: j.company_id,
            location: j.location || j.company_area || 'India',
            department: j.department || j.field || 'Engineering',
            url: j.url,
            atsName: j.ats_provider ? j.ats_provider.toUpperCase() : 'Direct ATS',
            experienceLevel: j.level,
            postedDate: j.created_at ? new Date(j.created_at).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }) : 'Today',
          }));
          setJobs(mappedJobs);
        })
        .catch(err => console.error('Failed to load jobs:', err))
        .finally(() => {
          if (isMounted) setLoading(false);
        });
    }, 250);

    return () => {
      isMounted = false;
      clearTimeout(debounceTimer);
    };
  }, [searchQuery, selectedLocation, selectedCategory]);

  const filteredJobs = jobs;

  return (
    <div className="relative min-h-screen text-ink py-8 px-4 sm:px-6 lg:px-8 max-w-7xl mx-auto flex flex-col gap-10">
      {/* Editorial Background Grid Lines */}
      <div className="absolute inset-0 pointer-events-none opacity-40 bg-[linear-gradient(to_right,#80808012_1px,transparent_1px)] bg-[size:120px]" />

      {/* Top Banner & Header */}
      <div className="relative text-center max-w-3xl mx-auto pt-4">
        <div className="inline-flex items-center gap-2 px-3.5 py-1.5 rounded-full bg-accent-soft text-accent text-xs font-semibold mb-4 border border-accent/20 shadow-xs">
          <Sparkles className="w-3.5 h-3.5" />
          <span>Pan-India Tech Job Discovery &amp; AI Copilot</span>
        </div>
        
        {/* Editorial Luxury Serif Heading */}
        <h1 className="text-4xl sm:text-6xl text-ink font-serif font-normal tracking-tight leading-[1.08] text-balance">
          Let <span className="font-bold text-accent italic">AI</span> find you the right tech job. <br />
          <span className="font-bold text-ink italic font-serif">Right Now.</span>
        </h1>
        
        <p className="text-sm sm:text-base text-ink-soft mt-4 max-w-xl mx-auto font-sans leading-relaxed">
          Aggregated directly from 24,000+ company ATS hiring boards. Zero reposts, zero ghost listings.
        </p>
      </div>

      {/* 1. Unified Search Capsule */}
      <UnifiedSearchCapsule
        searchQuery={searchQuery}
        onSearchChange={setSearchQuery}
        selectedLocation={selectedLocation}
        onLocationChange={setSelectedLocation}
        onSelectPill={pill => {
          if (pill === 'Vasai-Virar') setSelectedLocation('Vasai');
          else if (pill === 'Remote') setSelectedLocation('Remote');
          else setSearchQuery(pill);
        }}
      />

      {/* 2. Interactive Natural Language Preference Selector */}
      <PreferenceSentenceFilter
        selectedRole={selectedRole}
        onRoleChange={setSelectedRole}
        selectedExp={selectedExp}
        onExpChange={setSelectedExp}
        selectedWorkType={selectedWorkType}
        onWorkTypeChange={setSelectedWorkType}
        selectedLocation={selectedLocation}
        onLocationChange={setSelectedLocation}
        onApplyPreferences={() => {
          if (selectedRole) setSearchQuery(selectedRole);
        }}
      />

      {/* 2.5 Horizontal Trending Jobs Quick Carousel */}
      {jobs.length > 0 && (
        <div className="relative">
          <div className="flex items-center justify-between mb-3 px-1">
            <div className="flex items-center gap-2">
              <TrendingUp className="w-4 h-4 text-accent" />
              <span className="text-xs font-mono font-bold uppercase tracking-wider text-ink-faint">
                Trending Active Roles Today
              </span>
            </div>
          </div>
          <div className="flex gap-4 overflow-x-auto pb-3 pt-1 scrollbar-none snap-x">
            {jobs.slice(0, 6).map(j => (
              <div
                key={`trending-${j.id}`}
                onClick={() => j.companyId && handleSelectCompany(j.companyId)}
                className="min-w-[280px] sm:min-w-[320px] snap-start bg-paper dark:bg-zinc-900/90 border border-line hover:border-accent/50 rounded-2xl p-4 shadow-xs hover:shadow-md transition-all cursor-pointer flex flex-col justify-between gap-3 group"
              >
                <div className="flex items-start gap-3">
                  {j.companyDomain ? (
                    <img
                      src={`https://www.google.com/s2/favicons?domain=${j.companyDomain}&sz=128`}
                      alt={j.companyName}
                      className="w-9 h-9 rounded-xl border border-line object-contain bg-white flex-shrink-0 p-1"
                    />
                  ) : (
                    <div className="w-9 h-9 rounded-xl bg-accent-soft text-accent font-bold text-xs flex items-center justify-center font-mono">
                      {j.companyName.slice(0, 2)}
                    </div>
                  )}
                  <div className="min-w-0 flex-1">
                    <span className="text-[11px] font-semibold text-ink-faint truncate block">
                      {j.companyName}
                    </span>
                    <h4 className="text-xs sm:text-sm font-bold text-ink group-hover:text-accent transition-colors truncate">
                      {j.title}
                    </h4>
                  </div>
                </div>

                <div className="flex items-center justify-between text-[11px] pt-2 border-t border-line/60">
                  <span className="text-ink-soft truncate max-w-[150px]">
                    📍 {j.location || 'India'}
                  </span>
                  <button
                    type="button"
                    onClick={e => {
                      e.stopPropagation();
                      navigate(`/dashboard?practice_job=${encodeURIComponent(j.title)}&company=${encodeURIComponent(j.companyName)}`);
                    }}
                    className="text-[10px] font-semibold text-accent hover:underline flex items-center gap-1"
                  >
                    <Sparkles className="w-3 h-3" /> Practice
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* 3. View Mode Switcher (Stream vs Map vs Grid) */}
      <div className="flex flex-col sm:flex-row items-center justify-between gap-4 border-b border-line pb-4 pt-2">
        <div className="flex items-center gap-2">
          <span className="text-sm font-bold text-ink">
            {filteredJobs.length} Live Openings
          </span>
          <span className="text-xs text-ink-faint">
            across {companies.length} verified startups
          </span>
        </div>

        {/* Switcher Controls */}
        <div className="flex items-center gap-1.5 p-1 rounded-xl bg-paper dark:bg-zinc-900 border border-line shadow-xs">
          <button
            type="button"
            className="px-3 py-1.5 rounded-lg text-xs font-semibold bg-accent text-white shadow-xs flex items-center gap-1.5"
          >
            <Briefcase className="w-3.5 h-3.5" />
            <span>Jobs Stream</span>
          </button>
          <button
            type="button"
            onClick={() => navigate('/directory')}
            className="px-3 py-1.5 rounded-lg text-xs font-semibold text-ink-soft hover:text-ink hover:bg-zinc-100 dark:hover:bg-zinc-800 transition-colors flex items-center gap-1.5"
          >
            <Globe className="w-3.5 h-3.5" />
            <span>2D / 3D Job Map</span>
          </button>
        </div>
      </div>

      {/* 4. Main 3-Column Split Discovery Layout */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
        {/* Left Column: Popular Search Roles & Tech Sub-Hubs (3 cols) */}
        <div className="hidden lg:flex flex-col gap-5 lg:col-span-3 sticky top-24">
          {/* Categories Box */}
          <div className="bg-paper dark:bg-zinc-900/90 border border-line rounded-2xl p-4 shadow-xs">
            <h4 className="text-xs font-mono font-bold uppercase text-ink-faint tracking-wider mb-3 flex items-center gap-2">
              <Layers className="w-3.5 h-3.5 text-accent" />
              <span>Role Domains</span>
            </h4>
            <div className="flex flex-col gap-1">
              {POPULAR_CATEGORIES.map(cat => (
                <button
                  key={cat.label}
                  type="button"
                  onClick={() => setSelectedCategory(cat.value)}
                  className={`text-left px-3 py-2 rounded-xl text-xs font-medium transition-colors flex items-center justify-between ${
                    selectedCategory === cat.value
                      ? 'bg-accent-soft text-accent font-semibold'
                      : 'text-ink-soft hover:bg-zinc-100 dark:hover:bg-zinc-800 hover:text-ink'
                  }`}
                >
                  <span className="truncate">{cat.label}</span>
                  {selectedCategory === cat.value && (
                    <CheckCircle2 className="w-3.5 h-3.5 text-accent flex-shrink-0" />
                  )}
                </button>
              ))}
            </div>
          </div>

          {/* Sub-Hubs Box */}
          <div className="bg-paper dark:bg-zinc-900/90 border border-line rounded-2xl p-4 shadow-xs">
            <h4 className="text-xs font-mono font-bold uppercase text-ink-faint tracking-wider mb-3 flex items-center gap-2">
              <MapPin className="w-3.5 h-3.5 text-accent" />
              <span>Tech Sub-Hubs</span>
            </h4>
            <div className="flex flex-col gap-1">
              {SUB_HUBS.map(hub => (
                <button
                  key={hub.name}
                  type="button"
                  onClick={() => setSelectedLocation(hub.query)}
                  className={`text-left px-3 py-2 rounded-xl text-xs font-medium transition-colors flex items-center justify-between ${
                    selectedLocation === hub.query
                      ? 'bg-accent-soft text-accent font-semibold'
                      : 'text-ink-soft hover:bg-zinc-100 dark:hover:bg-zinc-800 hover:text-ink'
                  }`}
                >
                  <span className="truncate">{hub.name}</span>
                  {selectedLocation === hub.query && (
                    <CheckCircle2 className="w-3.5 h-3.5 text-accent flex-shrink-0" />
                  )}
                </button>
              ))}
            </div>
          </div>
        </div>

        {/* Center Column: Live Job Stream Cards (6 cols) */}
        <div className="lg:col-span-6 flex flex-col gap-3 min-w-0">
          {loading ? (
            <div className="flex flex-col items-center justify-center p-12 bg-paper dark:bg-zinc-900/40 border border-line rounded-2xl">
              <RefreshCw className="w-6 h-6 text-accent animate-spin mb-2" />
              <p className="text-xs font-mono text-ink-faint">Loading real-time job feeds...</p>
            </div>
          ) : filteredJobs.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-12 bg-paper dark:bg-zinc-900/40 border border-line rounded-2xl text-center">
              <Briefcase className="w-10 h-10 text-ink-faint mb-3" />
              <h3 className="text-sm font-bold text-ink">No matching openings found</h3>
              <p className="text-xs text-ink-soft mt-1">Try clearing filters or selecting another tech hub.</p>
              <button
                type="button"
                onClick={() => {
                  setSearchQuery('');
                  setSelectedLocation('');
                  setSelectedCategory('');
                }}
                className="mt-4 px-4 py-2 rounded-xl text-xs font-semibold bg-accent-soft text-accent hover:bg-accent hover:text-white transition-colors"
              >
                Reset All Filters
              </button>
            </div>
          ) : (
            filteredJobs.map(job => (
              <JobListingCard
                key={job.id}
                job={job}
                onSelectCompany={id => handleSelectCompany(id)}
              />
            ))
          )}
        </div>

        {/* Right Column: AI Matcher & Copilot Sidebar (3 cols) */}
        <div className="lg:col-span-3">
          <AiMatchSummaryCard
            totalJobsCount={totalLiveJobs}
            filteredCount={filteredJobs.length}
          />
        </div>
      </div>

      {/* Sliding Company Inspector Drawer */}
      <CompanyDrawer
        company={selectedCompany}
        onClose={() => setSelectedCompany(null)}
      />
    </div>
  );
}
