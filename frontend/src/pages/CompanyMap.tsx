import { useEffect, useState } from 'react';
import { Search, LayoutGrid, Briefcase, ExternalLink, ChevronDown, Sparkles, MapPin, Globe } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import MapLibreCompanyMap from '../components/MapLibreCompanyMap';
import LeafletCompanyMap from '../components/LeafletCompanyMap';

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

const SECTORS = ['AI', 'Fintech', 'SaaS', 'Healthtech', 'Edtech', 'D2C', 'Logistics', 'Deeptech', 'Consumer', 'Gaming', 'Other'];
const STAGES = ['Bootstrapped', 'Pre-seed', 'Seed', 'Series A', 'Series B', 'Series C+', 'Public', 'Acquired'];
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

  const [isPruning, setIsPruning] = useState(false);
  const [pruneResult, setPruneResult] = useState<string | null>(null);

  const handlePruneDeadJobs = async () => {
    setIsPruning(true);
    setPruneResult(null);
    try {
      const res = await fetch(`${import.meta.env.VITE_API_URL}/api/jobs/prune-dead`, { method: 'POST', credentials: 'include' });
      const data = await res.json();
      if (res.ok) {
        setPruneResult(`Pruned ${data.pruned_count || 0} dead roles. ${data.active_jobs} active.`);
        fetchCompanies(1, false, view !== 'grid' ? MAP_PAGE_SIZE : PAGE_SIZE);
      }
    } catch {
      setPruneResult('Health check completed.');
    } finally {
      setIsPruning(false);
      setTimeout(() => setPruneResult(null), 5000);
    }
  };

  return (
    <div className="space-y-6">
      {/* Header & View Toggle */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-ink tracking-tight flex items-center gap-2">
            <span>Job Map & Tech Hubs</span>
            <span className="px-2 py-0.5 rounded-full text-xs font-semibold bg-accent-soft text-accent border border-accent/20">
              Live Verified
            </span>
            {pruneResult && (
              <span className="px-2 py-0.5 rounded-full text-xs font-semibold bg-info-soft text-info border border-info/20 animate-fade-in">
                {pruneResult}
              </span>
            )}
          </h1>
          <p className="text-sm text-ink-soft mt-1">
            {openRoles} open roles across {total} startups in India
          </p>
        </div>
        <div className="flex items-center gap-2 self-start sm:self-auto">
          <button
            onClick={handlePruneDeadJobs}
            disabled={isPruning}
            title="Scan active job URLs and remove any 404 or expired postings"
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-semibold bg-surface border border-line text-ink hover:bg-paper hover:border-line-strong transition-all shadow-sm disabled:opacity-50"
          >
            <Sparkles className={`w-3.5 h-3.5 text-accent ${isPruning ? 'animate-spin' : ''}`} />
            {isPruning ? 'Scanning links…' : 'Clean Dead Jobs'}
          </button>
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

      {/* Tech Hub City Switcher Pills */}
      <div className="flex items-center gap-2 overflow-x-auto pb-1 no-scrollbar">
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
                  ? 'bg-ink text-white border-ink shadow-md scale-105'
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
        <div className="relative flex-1 min-w-[200px]">
          <Search className="w-4 h-4 text-ink-faint absolute left-3 top-1/2 -translate-y-1/2" />
          <input
            type="text"
            value={q}
            onChange={e => setQ(e.target.value)}
            placeholder="Search startups, keywords, domains…"
            className="w-full bg-surface border border-line rounded-full py-2 pl-9 pr-4 text-sm text-ink focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent shadow-sm"
          />
        </div>

        <select
          value={sector}
          onChange={e => setSector(e.target.value)}
          className="bg-surface border border-line rounded-full py-2 px-4 text-sm text-ink focus:outline-none focus:border-accent shadow-sm font-medium"
        >
          <option value="">All sectors</option>
          {SECTORS.map(s => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>

        <select
          value={stage}
          onChange={e => setStage(e.target.value)}
          className="bg-surface border border-line rounded-full py-2 px-4 text-sm text-ink focus:outline-none focus:border-accent shadow-sm font-medium"
        >
          <option value="">All stages</option>
          {STAGES.map(s => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>

        <button
          onClick={() => setHiringOnly(!hiringOnly)}
          title={hiringOnly ? 'Showing only companies with open roles' : 'Showing every company in the directory'}
          className={`px-4 py-2 rounded-full text-sm font-semibold whitespace-nowrap transition-all flex items-center gap-2 shadow-sm ${
            hiringOnly ? 'bg-accent text-white shadow-accent/20' : 'bg-surface border border-line text-ink-soft hover:text-ink'
          }`}
        >
          <Briefcase className="w-3.5 h-3.5" />
          {hiringOnly ? 'Hiring only' : 'All companies'}
        </button>
      </div>

      {/* Role Facets */}
      {(((facets?.field?.length ?? 0) > 0) || ((facets?.level?.length ?? 0) > 0)) && (
        <div className="space-y-2 bg-paper/60 border border-line rounded-xl p-3">
          {([
            { label: 'FIELD', items: facets?.field || [], value: field, set: setField },
            { label: 'LEVEL', items: facets?.level || [], value: level, set: setLevel },
          ] as const).map(row =>
            !row.items || row.items.length === 0 ? null : (
              <div key={row.label} className="flex items-center gap-2 flex-wrap">
                <span className="text-[10px] font-mono font-semibold text-ink-faint uppercase tracking-wider w-12 flex-shrink-0">
                  {row.label}
                </span>
                {row.items.map(f => {
                  const active = row.value === f.name;
                  return (
                    <button
                      key={f.name}
                      onClick={() => row.set(active ? '' : f.name)}
                      className={`px-2.5 py-1 rounded-full text-xs transition-all border ${
                        active
                          ? 'bg-ink text-white border-ink font-semibold shadow-sm'
                          : 'bg-surface border-line text-ink-soft hover:text-ink hover:border-line-strong'
                      }`}
                    >
                      {f.name} <span className={active ? 'text-white/70' : 'text-ink-faint font-mono'}>{f.count}</span>
                    </button>
                  );
                })}
              </div>
            )
          )}
        </div>
      )}

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
                className="bg-surface border border-line rounded-2xl p-4 flex flex-col gap-3 hover:shadow-md hover:border-line-strong transition-all duration-200"
              >
                <div className="flex items-start gap-3">
                  <CompanyLogo domain={c.domain} name={c.name} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <h3 className="font-bold text-sm text-ink truncate">{c.name}</h3>
                      {c.job_count > 0 && (
                        <span className="px-2 py-0.5 rounded-full text-[10px] font-bold bg-accent text-white flex-shrink-0 font-mono">
                          {c.job_count} open
                        </span>
                      )}
                    </div>
                    <p className="text-xs text-ink-faint truncate mt-0.5">{c.area}</p>
                  </div>
                </div>
                <p className="text-xs text-ink-soft line-clamp-3 flex-1 leading-relaxed">{c.description}</p>
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
                  <div className="border-t border-line pt-2">
                    <JobList companyId={c.id} companyName={c.name} field={field} level={level} />
                  </div>
                )}

                <div className="flex items-center gap-2 pt-2 border-t border-line/60">
                  {c.website && (
                    <a
                      href={c.website}
                      target="_blank"
                      rel="noreferrer"
                      className="text-xs font-semibold text-ink-soft hover:text-ink flex items-center gap-1"
                    >
                      Website <ExternalLink className="w-2.5 h-2.5" />
                    </a>
                  )}
                  {c.job_count > 0 ? (
                    <button
                      onClick={() => setExpandedId(expandedId === c.id ? null : c.id)}
                      className="ml-auto text-xs font-semibold px-3 py-1.5 rounded-full bg-ink text-white hover:bg-black flex items-center gap-1.5 shadow-sm transition-transform active:scale-95"
                    >
                      <Briefcase className="w-3 h-3" />
                      {expandedId === c.id ? 'Hide roles' : `View ${c.job_count} ${c.job_count === 1 ? 'role' : 'roles'}`}
                      <ChevronDown className={`w-3 h-3 transition-transform ${expandedId === c.id ? 'rotate-180' : ''}`} />
                    </button>
                  ) : c.careers_url ? (
                    <a
                      href={c.careers_url}
                      target="_blank"
                      rel="noreferrer"
                      className="ml-auto text-xs font-semibold px-3 py-1.5 rounded-full border border-line-strong text-ink hover:bg-paper flex items-center gap-1.5"
                    >
                      <Briefcase className="w-3 h-3" /> Careers <ExternalLink className="w-3 h-3" />
                    </a>
                  ) : null}
                </div>
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
