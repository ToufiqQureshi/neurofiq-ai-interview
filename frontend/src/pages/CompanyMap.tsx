import { useEffect, useState } from 'react';
import { Search, LayoutGrid, Briefcase, ExternalLink, ChevronDown, Sparkles, MapPin, Globe } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import MapLibreCompanyMap from '../components/MapLibreCompanyMap';
import LeafletCompanyMap from '../components/LeafletCompanyMap';
import { CustomDropdown } from '../components/CustomDropdown';

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

interface Facet {
  name: string;
  count: number;
}

interface Job {
  id: string;
  title: string;
  department: string;
  location: string;
  url: string;
}

interface TechHub {
  id: string;
  name: string;
  query: string;
  lat: number;
  lng: number;
  zoom: number;
  minZoom: number;
  maxZoom: number;
  bounds: [[number, number], [number, number]];
  icon: string;
}

const TECH_HUBS: TechHub[] = [
  {
    id: 'all',
    name: 'All Hubs',
    query: '',
    lat: 21.7679,
    lng: 78.8718,
    zoom: 5,
    minZoom: 4,
    maxZoom: 18,
    bounds: [[68.0, 6.5], [97.5, 35.5]],
    icon: '🇮🇳',
  },
  {
    id: 'bengaluru',
    name: 'Bengaluru',
    query: 'Bengaluru',
    lat: 12.9716,
    lng: 77.5946,
    zoom: 12,
    minZoom: 10,
    maxZoom: 18,
    bounds: [[77.35, 12.70], [77.90, 13.25]],
    icon: '🚀',
  },
  {
    id: 'delhi_ncr',
    name: 'Delhi NCR (Noida/Gurgaon)',
    query: 'Delhi NCR',
    lat: 28.5355,
    lng: 77.3910,
    zoom: 11,
    minZoom: 10,
    maxZoom: 18,
    bounds: [[76.70, 28.15], [77.75, 29.00]],
    icon: '🏛️',
  },
  {
    id: 'mumbai',
    name: 'Mumbai',
    query: 'Mumbai',
    lat: 19.0760,
    lng: 72.8777,
    zoom: 12,
    minZoom: 10,
    maxZoom: 18,
    bounds: [[72.60, 18.75], [73.25, 19.45]],
    icon: '🌊',
  },
  {
    id: 'hyderabad',
    name: 'Hyderabad',
    query: 'Hyderabad',
    lat: 17.3850,
    lng: 78.4867,
    zoom: 12,
    minZoom: 10,
    maxZoom: 18,
    bounds: [[78.10, 17.10], [78.75, 17.70]],
    icon: '💎',
  },
  {
    id: 'pune',
    name: 'Pune',
    query: 'Pune',
    lat: 18.5204,
    lng: 73.8567,
    zoom: 12,
    minZoom: 10,
    maxZoom: 18,
    bounds: [[73.55, 18.25], [74.15, 18.85]],
    icon: '⚡',
  },
];

// Sector and stage options come from /api/companies, which reads them off the
// directory itself. They used to be two lists typed here, and both had drifted
// away from the data: they offered Pre-seed, Gaming, Consumer and Other, which
// no company has, while Series C, Series H and the 145 companies with no
// recorded stage at all could not be selected from any option shown.
const PAGE_SIZE = 24;
const MAP_PAGE_SIZE = 500;

function CompanyLogo({ domain, name }: { domain: string; name: string }) {
  const [failed, setFailed] = useState(false);
  if (failed || !domain) {
    return (
      <div className="w-10 h-10 rounded-xl bg-paper border border-line flex items-center justify-center text-ink-soft text-xs font-mono font-bold uppercase flex-shrink-0 shadow-sm">
        {name.slice(0, 2)}
      </div>
    );
  }
  return (
    <img
      src={`https://www.google.com/s2/favicons?domain=${domain}&sz=128`}
      alt={name}
      className="w-10 h-10 rounded-xl border border-line object-contain bg-white flex-shrink-0 p-1 shadow-sm"
      onError={() => setFailed(true)}
      onLoad={e => {
        if (e.currentTarget.naturalWidth < 32) setFailed(true);
      }}
    />
  );
}

// Lazily fetches and renders a company's real open roles with 1-Click Practice Mock Interview
function JobList({
  companyId,
  companyName,
  field,
  level,
}: {
  companyId: string;
  companyName: string;
  field?: string;
  level?: string;
}) {
  const [jobs, setJobs] = useState<Job[] | null>(null);
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();

  useEffect(() => {
    let cancelled = false;
    const params = new URLSearchParams();
    if (field) params.set('field', field);
    if (level) params.set('level', level);
    const qs = params.toString() ? `?${params.toString()}` : '';
    fetch(`${import.meta.env.VITE_API_URL}/api/companies/${companyId}/jobs${qs}`, { credentials: 'include' })
      .then(r => (r.ok ? r.json() : null))
      .then(d => {
        if (!cancelled) setJobs(d?.jobs || []);
      })
      .catch(() => {
        if (!cancelled) setJobs([]);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [companyId, field, level]);

  if (loading) return <p className="text-xs text-ink-faint py-2 font-mono">Loading open roles…</p>;
  if (!jobs || jobs.length === 0) {
    return <p className="text-xs text-ink-faint py-2">No open roles listed right now.</p>;
  }

  return (
    <ul className="divide-y divide-line max-h-64 overflow-y-auto -mx-1 pr-1">
      {jobs.map(j => (
        <li key={j.id} className="py-2 px-1 rounded-lg hover:bg-paper/80 transition-colors">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0 flex-1">
              <p className="text-xs font-semibold text-ink truncate">{j.title}</p>
              <p className="text-[10px] text-ink-faint truncate">
                {[j.department, j.location].filter(Boolean).join(' · ')}
              </p>
            </div>
            <div className="flex items-center gap-1.5 flex-shrink-0">
              <button
                onClick={e => {
                  e.stopPropagation();
                  navigate('/dashboard');
                }}
                title={`Practice AI Interview for ${j.title} at ${companyName}`}
                className="px-2 py-0.5 rounded-full text-[10px] font-semibold bg-accent-soft text-accent hover:bg-accent hover:text-white transition-all flex items-center gap-1"
              >
                <Sparkles className="w-2.5 h-2.5" /> Practice
              </button>
              <a
                href={j.url}
                target="_blank"
                rel="noreferrer"
                title="Open live career posting"
                className="p-1 rounded text-ink-faint hover:text-ink hover:bg-line transition-colors"
              >
                <ExternalLink className="w-3 h-3" />
              </a>
            </div>
          </div>
        </li>
      ))}
    </ul>
  );
}

interface DirectoryStats {
  companies: number;
  hiring_companies: number;
  jobs: number;
  new_jobs_24h: number;
  new_jobs_7d: number;
  last_job_at: string | null;
}

const STATS_POLL_MS = 60_000;

function timeAgo(iso: string): string {
  const mins = Math.floor((Date.now() - new Date(iso).getTime()) / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

export function CompanyMap() {
  const [companies, setCompanies] = useState<Company[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [view, setView] = useState<'grid' | 'map2d' | 'map3d'>('map3d');
  // The directory is a map of the ecosystem, not a jobs board, so the
  // default view is the whole thing and "Hiring only" is the overlay you
  // reach for. This used to default the other way, back when the aim was to
  // never show an empty card. Most companies are not hiring at any given
  // moment — in a comparable public directory only 9% were — so defaulting
  // to hiring-only was quietly hiding the map itself.
  const [hiringOnly, setHiringOnly] = useState(false);
  const [openRoles, setOpenRoles] = useState(0);
  const [facets, setFacets] = useState<{ field: Facet[]; level: Facet[] }>({ field: [], level: [] });
  const [sectors, setSectors] = useState<string[]>([]);
  const [stages, setStages] = useState<string[]>([]);
  const [field, setField] = useState('');
  const [level, setLevel] = useState('');
  const [page, setPage] = useState(1);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [stats, setStats] = useState<DirectoryStats | null>(null);

  // Tech Hub state
  const [selectedHub, setSelectedHub] = useState<TechHub>(TECH_HUBS[0]);
  const [sector, setSector] = useState('');
  const [stage, setStage] = useState('');
  const [area, setArea] = useState('');
  const [q, setQ] = useState('');

  function buildURL(pageNum: number, pageSize: number) {
    const params = new URLSearchParams({ page: String(pageNum), page_size: String(pageSize) });
    if (sector) params.set('sector', sector);
    if (stage) params.set('stage', stage);
    
    // Use selected Tech Hub query if active, else custom area input
    const effectiveArea = selectedHub.query || area;
    if (effectiveArea) params.set('area', effectiveArea);
    
    if (q) params.set('q', q);
    if (hiringOnly) params.set('hiring', '1');
    return `${import.meta.env.VITE_API_URL}/api/companies?${params.toString()}`;
  }

  function fetchCompanies(pageNum: number, append: boolean, pageSize: number) {
    setLoading(true);
    fetch(buildURL(pageNum, pageSize), { credentials: 'include' })
      .then(r => (r.ok ? r.json() : null))
      .then(d => {
        if (!d) return;
        setCompanies(prev => (append ? [...prev, ...(d.companies || [])] : d.companies || []));
        setTotal(d.total || 0);
        setOpenRoles(d.open_roles || 0);
        // Unlike field and level, these two do not follow the current
        // filters — they are the options available, not the options left.
        setSectors(Array.isArray(d.facets?.sector) ? d.facets.sector : []);
        setStages(Array.isArray(d.facets?.stage) ? d.facets.stage : []);
        setFacets({
          field: Array.isArray(d.facets?.field) ? d.facets.field : [],
          level: Array.isArray(d.facets?.level) ? d.facets.level : [],
        });
      })
      .catch(console.error)
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    let cancelled = false;
    const load = () =>
      fetch(`${import.meta.env.VITE_API_URL}/api/companies/stats`, { credentials: 'include' })
        .then(r => (r.ok ? r.json() : null))
        .then(d => {
          if (!cancelled && d) setStats(d);
        })
        .catch(() => {});
    load();
    const id = setInterval(load, STATS_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  useEffect(() => {
    const timer = setTimeout(() => {
      setPage(1);
      fetchCompanies(1, false, view !== 'grid' ? MAP_PAGE_SIZE : PAGE_SIZE);
    }, 150);
    return () => clearTimeout(timer);
  }, [sector, stage, area, selectedHub, q, hiringOnly, view]);

  const canLoadMore = companies.length < total;



  return (
    <div className="p-4 sm:p-6 lg:p-8 max-w-[1600px] mx-auto space-y-6">
      {/* Header & View Toggle */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-2 border-b border-line/40">
        <div>
          <h1 className="text-2xl font-bold text-ink tracking-tight flex items-center gap-2">
            <span>Job Map & Tech Hubs</span>
            <span className="px-2.5 py-0.5 rounded-full text-xs font-semibold bg-accent-soft text-accent border border-accent/20 shadow-2xs">
              Live Verified
            </span>
          </h1>
          <p className="text-sm text-ink-soft mt-1">
            {openRoles} open roles across {total} startups in India
          </p>
        </div>
        <div className="flex items-center gap-2 self-start sm:self-auto">
          <div className="flex items-center gap-1 bg-surface border border-line rounded-full p-1 shadow-sm">
            <button
              onClick={() => setView('grid')}
              className={`flex items-center gap-1.5 px-3.5 py-1.5 rounded-full text-xs font-semibold transition-all ${
                view === 'grid' ? 'bg-ink text-white shadow-sm' : 'text-ink-soft hover:text-ink'
              }`}
            >
              <LayoutGrid className="w-3.5 h-3.5" /> Grid
            </button>
            <button
              onClick={() => setView('map2d')}
              className={`flex items-center gap-1.5 px-3.5 py-1.5 rounded-full text-xs font-semibold transition-all ${
                view === 'map2d' ? 'bg-ink text-white shadow-sm' : 'text-ink-soft hover:text-ink'
              }`}
            >
              <Globe className="w-3.5 h-3.5 text-emerald-500" /> 2D Map
            </button>
            <button
              onClick={() => setView('map3d')}
              className={`flex items-center gap-1.5 px-3.5 py-1.5 rounded-full text-xs font-semibold transition-all ${
                view === 'map3d' ? 'bg-ink text-white shadow-sm' : 'text-ink-soft hover:text-ink'
              }`}
            >
              <Sparkles className="w-3.5 h-3.5 text-amber-400" /> 3D Map
            </button>
          </div>
        </div>
      </div>

      {/* Directory metrics counter - shown in Grid view */}
      {view === 'grid' && stats && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 animate-fade-in">
          <div className="bg-surface border border-line rounded-xl p-3.5 shadow-sm">
            <p className="text-[10px] font-mono uppercase tracking-wider text-ink-faint">COMPANIES</p>
            <p className="text-2xl font-bold text-ink mt-0.5">{stats.companies}</p>
            <p className="text-[11px] text-ink-soft">{stats.hiring_companies} hiring now</p>
          </div>
          <div className="bg-surface border border-line rounded-xl p-3.5 shadow-sm">
            <p className="text-[10px] font-mono uppercase tracking-wider text-ink-faint">OPEN ROLES</p>
            <p className="text-2xl font-bold text-ink mt-0.5">{stats.jobs}</p>
            <p className="text-[11px] text-ink-soft">{stats.new_jobs_7d} added this week</p>
          </div>
          <div className="bg-surface border border-line rounded-xl p-3.5 shadow-sm">
            <p className="text-[10px] font-mono uppercase tracking-wider text-ink-faint">NEW IN 24H</p>
            <p className="text-2xl font-bold text-accent mt-0.5">+{stats.new_jobs_24h}</p>
            <p className="text-[11px] text-ink-soft">fresh postings</p>
          </div>
          <div className="bg-surface border border-line rounded-xl p-3.5 shadow-sm">
            <p className="text-[10px] font-mono uppercase tracking-wider text-ink-faint">LAST SYNCED</p>
            <p className="text-2xl font-bold text-ink mt-0.5">{stats.last_job_at ? timeAgo(stats.last_job_at) : '—'}</p>
            <p className="text-[11px] text-ink-soft">hourly ATS sync</p>
          </div>
        </div>
      )}

      {/* Interactive Controls & Filters */}
      <div className="space-y-4">
        {/* Tech Hub City Switcher Pills */}
        <div className="flex items-center gap-2.5 overflow-x-auto pb-1 no-scrollbar">
          <span className="text-xs font-mono font-semibold text-ink-faint uppercase tracking-wider flex items-center gap-1 flex-shrink-0 mr-1">
            <MapPin className="w-3.5 h-3.5 text-accent" /> Tech Hub:
          </span>
          {TECH_HUBS.map(hub => {
            const active = selectedHub.id === hub.id;
            return (
              <button
                key={hub.id}
                onClick={() => {
                  setSelectedHub(hub);
                  setArea('');
                }}
                className={`px-3.5 py-1.5 rounded-full text-xs font-semibold transition-all flex items-center gap-1.5 flex-shrink-0 border ${
                  active
                    ? 'bg-ink text-white border-ink shadow-md scale-102'
                    : 'bg-surface border-line text-ink-soft hover:text-ink hover:border-line-strong hover:bg-paper'
                }`}
              >
                <span>{hub.icon}</span>
                <span>{hub.name}</span>
              </button>
            );
          })}
        </div>

        {/* Main Filter Bar */}
        <div className="flex items-center gap-3 flex-wrap">
          <div className="relative flex-1 min-w-[220px]">
            <Search className="w-4 h-4 text-ink-faint absolute left-3.5 top-1/2 -translate-y-1/2" />
            <input
              type="text"
              value={q}
              onChange={e => setQ(e.target.value)}
              placeholder="Search startups, keywords, domains…"
              className="w-full appearance-none bg-surface border border-line rounded-full py-2.5 pl-10 pr-4 text-sm text-ink placeholder:text-ink-faint focus:outline-none focus:border-accent focus:ring-2 focus:ring-accent/20 shadow-2xs font-medium hover:border-line-strong transition-colors"
            />
          </div>

          <CustomDropdown
            value={sector}
            onChange={setSector}
            options={[{ label: 'All sectors', value: '' }, ...sectors.map(s => ({ label: s, value: s }))]}
            placeholder="All sectors"
          />

          <CustomDropdown
            value={stage}
            onChange={setStage}
            options={[{ label: 'All stages', value: '' }, ...stages.map(s => ({ label: s, value: s }))]}
            placeholder="All stages"
          />

          <button
            onClick={() => setHiringOnly(!hiringOnly)}
            title={hiringOnly ? 'Showing only companies with open roles' : 'Showing every company in the directory'}
            className={`px-4 py-2.5 rounded-full text-sm font-bold whitespace-nowrap transition-all flex items-center gap-2 shadow-2xs ${
              hiringOnly ? 'bg-accent text-white shadow-accent/20 ring-2 ring-accent/30' : 'bg-surface border border-line text-ink-soft hover:text-ink hover:border-line-strong'
            }`}
          >
            <Briefcase className="w-3.5 h-3.5" />
            {hiringOnly ? 'Hiring only' : 'All companies'}
          </button>
        </div>

        {/* Role Facets */}
        {(((facets?.field?.length ?? 0) > 0) || ((facets?.level?.length ?? 0) > 0)) && (
          <div className="space-y-3 bg-surface border border-line rounded-2xl p-4 shadow-2xs">
            {([
              { label: 'FIELD', items: facets?.field || [], value: field, set: setField },
              { label: 'LEVEL', items: facets?.level || [], value: level, set: setLevel },
            ] as const).map(row =>
              !row.items || row.items.length === 0 ? null : (
                <div key={row.label} className="flex items-center gap-3 flex-wrap">
                  <span className="text-[11px] font-mono font-bold text-ink-faint uppercase tracking-wider w-14 flex-shrink-0">
                    {row.label}
                  </span>
                  <div className="flex items-center gap-2 flex-wrap flex-1">
                    {row.items.map(f => {
                      const active = row.value === f.name;
                      return (
                        <button
                          key={f.name}
                          onClick={() => row.set(active ? '' : f.name)}
                          className={`px-3 py-1.5 rounded-full text-xs font-medium transition-all border flex items-center gap-1.5 shadow-2xs ${
                            active
                              ? 'bg-ink text-white border-ink font-semibold shadow-sm'
                              : 'bg-paper/70 border-line text-ink-soft hover:text-ink hover:border-line-strong hover:bg-paper'
                          }`}
                        >
                          <span>{f.name}</span>
                          <span className={`text-[10px] px-1.5 py-0.5 rounded-full font-mono font-semibold ${
                            active ? 'bg-white/20 text-white' : 'bg-line/40 text-ink-soft'
                          }`}>
                            {f.count}
                          </span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              )
            )}
          </div>
        )}
      </div>


      {/* Map View vs Grid View */}
      {view === 'map3d' ? (
        <MapLibreCompanyMap companies={companies} selectedHub={selectedHub} />
      ) : view === 'map2d' ? (
        <LeafletCompanyMap companies={companies} selectedHub={selectedHub} />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {loading && companies.length === 0 ? (
            <div className="col-span-full p-12 text-center text-sm text-ink-faint">Loading companies...</div>
          ) : companies.length === 0 ? (
            <div className="col-span-full p-12 text-center text-sm text-ink-faint bg-surface border border-line rounded-2xl">
              No companies found for this hub or filter combination.
            </div>
          ) : (
            companies.map(c => (
              <div
                key={c.id}
                className="relative bg-white/40 backdrop-blur-md border border-white/60 rounded-2xl p-5 flex flex-col gap-3 hover:-translate-y-1 hover:shadow-xl hover:shadow-accent/10 hover:border-accent/40 transition-all duration-300 overflow-hidden group"
              >
                {/* Subtle gradient background effect */}
                <div className="absolute inset-0 bg-gradient-to-br from-white/40 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300 pointer-events-none" />
                
                <div className="relative z-10 flex flex-col h-full gap-3">
                  <div className="flex items-start gap-3">
                  <CompanyLogo domain={c.domain} name={c.name} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <h3 className="font-bold text-sm text-ink truncate">{c.name}</h3>
                      {c.job_count > 0 && (
                        <span className="px-2.5 py-0.5 rounded-full text-[10px] font-bold bg-accent text-white flex-shrink-0 font-mono shadow-sm shadow-accent/30 ring-2 ring-white">
                          {c.job_count} open
                        </span>
                      )}
                    </div>
                    <p className="text-xs text-ink-faint truncate mt-0.5">{c.area}</p>
                  </div>
                </div>
                <p className="text-xs text-ink-soft line-clamp-2 min-h-[34px] flex-1 leading-relaxed">
                  {c.description || `${c.name} is an active Indian tech company hiring in ${c.area || 'India'} across ${c.sector || 'technology & business'} roles.`}
                </p>
                <div className="flex items-center gap-1.5 flex-wrap">
                  {c.sector && (
                    <span className="px-2.5 py-0.5 rounded-full text-[10px] font-semibold bg-accent-soft text-accent">
                      {c.sector}
                    </span>
                  )}
                  {c.stage && (
                    <span className="px-2.5 py-0.5 rounded-full text-[10px] font-semibold bg-info-soft text-info">
                      {c.stage}
                    </span>
                  )}
                </div>

                {expandedId === c.id && (
                  <div className="border-t border-line/40 pt-4 mt-2 bg-paper/50 rounded-b-2xl">
                    <JobList companyId={c.id} companyName={c.name} field={field} level={level} />
                  </div>
                )}
                               <div className={`flex items-center gap-2 pt-3 border-t border-line/40 mt-auto ${expandedId === c.id ? 'hidden' : ''}`}>
                  {c.website && (
                    <a
                      href={c.website}
                      target="_blank"
                      rel="noreferrer"
                      className="text-[11px] font-semibold text-ink-soft hover:text-accent flex items-center gap-1 transition-colors"
                    >
                      Website <ExternalLink className="w-2.5 h-2.5" />
                    </a>
                  )}
                  {c.job_count > 0 ? (
                    <button
                      onClick={() => setExpandedId(expandedId === c.id ? null : c.id)}
                      className="ml-auto text-[11px] font-bold px-4 py-2 rounded-full bg-ink text-white hover:bg-accent flex items-center gap-1.5 shadow-md hover:shadow-lg transition-all duration-300 active:scale-95 group-hover:scale-105"
                    >
                      <Briefcase className="w-3.5 h-3.5" />
                      View {c.job_count} {c.job_count === 1 ? 'role' : 'roles'}
                      <ChevronDown className="w-3.5 h-3.5" />
                    </button>
                  ) : c.careers_url ? (
                    <a
                      href={c.careers_url}
                      target="_blank"
                      rel="noreferrer"
                      className="ml-auto text-[11px] font-bold px-4 py-2 rounded-full bg-surface border border-line hover:border-ink text-ink flex items-center gap-1.5 transition-all duration-300 active:scale-95 group-hover:scale-105"
                    >
                      Careers <ExternalLink className="w-3.5 h-3.5" />
                    </a>
                  ) : null}
                </div>
                
                </div> {/* Close relative z-10 wrapper */}
              </div>
            ))
          )}
        </div>
      )}

      {view === 'grid' && canLoadMore && (
        <div className="flex justify-center pt-2">
          <button
            onClick={() => {
              const next = page + 1;
              setPage(next);
              fetchCompanies(next, true, PAGE_SIZE);
            }}
            disabled={loading}
            className="text-sm font-semibold px-6 py-2.5 rounded-full border border-line-strong text-ink hover:bg-paper transition-all disabled:opacity-50 shadow-sm"
          >
            {loading ? 'Loading...' : 'Load more startups'}
          </button>
        </div>
      )}
    </div>
  );
}
