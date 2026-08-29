import { useEffect, useMemo, useState } from 'react';
import { Search, LayoutGrid, MapIcon as MapViewIcon, Briefcase, ExternalLink, ChevronDown } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { MapContainer, TileLayer, Marker, Popup, useMap } from 'react-leaflet';
import MarkerClusterGroup from 'react-leaflet-cluster';
import L from 'leaflet';
import 'leaflet/dist/leaflet.css';
import 'leaflet.markercluster/dist/MarkerCluster.css';
import 'leaflet.markercluster/dist/MarkerCluster.Default.css';

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

const SECTORS = ['AI', 'Fintech', 'SaaS', 'Healthtech', 'Edtech', 'D2C', 'Logistics', 'Deeptech', 'Consumer', 'Gaming', 'Other'];
const STAGES = ['Bootstrapped', 'Pre-seed', 'Seed', 'Series A', 'Series B', 'Series C+', 'Public', 'Acquired'];
const PAGE_SIZE = 24;
// The map isn't paginated like the grid — it always loads the full matching
// set (up to this cap) so it never silently shows fewer pins than the grid.
const MAP_PAGE_SIZE = 500;

const pinIcon = L.divIcon({
  className: '',
  html: `<div style="width:14px;height:14px;border-radius:9999px;background:#15803d;border:2px solid white;box-shadow:0 0 0 1px #15803d;"></div>`,
  iconSize: [14, 14],
  iconAnchor: [7, 7],
});

function FitBounds({ positions }: { positions: [number, number][] }) {
  const map = useMap();
  useEffect(() => {
    if (positions.length === 0) return;
    if (positions.length === 1) {
      map.setView(positions[0], 11);
    } else {
      map.fitBounds(positions, { padding: [40, 40], maxZoom: 10 });
    }
  }, [positions, map]);
  return null;
}

function CompanyLogo({ domain, name }: { domain: string; name: string }) {
  const [failed, setFailed] = useState(false);
  if (failed || !domain) {
    return (
      <div className="w-10 h-10 rounded-lg bg-paper border border-line flex items-center justify-center text-ink-soft text-xs font-mono font-bold uppercase flex-shrink-0">
        {name.slice(0, 2)}
      </div>
    );
  }
  return (
    <img
      src={`https://www.google.com/s2/favicons?domain=${domain}&sz=128`}
      alt={name}
      className="w-10 h-10 rounded-lg border border-line object-contain bg-white flex-shrink-0"
      onError={() => setFailed(true)}
      onLoad={e => {
        // Google sometimes returns a generic 16x16 globe placeholder with a
        // 404 status when it has no real favicon for the domain — the <img>
        // still decodes it successfully so onError never fires. A genuine
        // favicon returned at the requested size (128) is much larger, so
        // treat a suspiciously small image as a miss too.
        if (e.currentTarget.naturalWidth < 32) setFailed(true);
      }}
    />
  );
}

// Lazily fetches and renders a company's real open roles (synced from its
// Greenhouse/Lever board) when the user opens it.
function JobList({ companyId, field, level }: { companyId: string; field?: string; level?: string }) {
  const [jobs, setJobs] = useState<Job[] | null>(null);
  const [loading, setLoading] = useState(true);

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

  if (loading) return <p className="text-xs text-ink-faint py-2">Loading roles…</p>;
  if (!jobs || jobs.length === 0) {
    return <p className="text-xs text-ink-faint py-2">No open roles listed right now.</p>;
  }

  return (
    <ul className="divide-y divide-line max-h-56 overflow-y-auto -mx-1">
      {jobs.map(j => (
        <li key={j.id}>
          <a
            href={j.url}
            target="_blank"
            rel="noreferrer"
            className="flex items-start justify-between gap-2 px-1 py-2 hover:bg-paper/60 rounded transition-colors group"
          >
            <div className="min-w-0">
              <p className="text-xs font-semibold text-ink group-hover:text-accent truncate">{j.title}</p>
              <p className="text-[10px] text-ink-faint truncate">
                {[j.department, j.location].filter(Boolean).join(' · ')}
              </p>
            </div>
            <ExternalLink className="w-3 h-3 text-ink-faint flex-shrink-0 mt-0.5" />
          </a>
        </li>
      ))}
    </ul>
  );
}

export function CompanyMap() {
  const [companies, setCompanies] = useState<Company[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [view, setView] = useState<'grid' | 'map'>('grid');
  // Most companies aren't hiring at any given moment, so default to the
  // roles that actually exist — browsing the full directory is opt-out.
  const [hiringOnly, setHiringOnly] = useState(true);
  const [openRoles, setOpenRoles] = useState(0);
  const [facets, setFacets] = useState<{ field: Facet[]; level: Facet[] }>({ field: [], level: [] });
  const [field, setField] = useState('');
  const [level, setLevel] = useState('');
  const [page, setPage] = useState(1);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const [sector, setSector] = useState('');
  const [stage, setStage] = useState('');
  const [area, setArea] = useState('');
  const [q, setQ] = useState('');
  const navigate = useNavigate();

  function buildURL(pageNum: number, pageSize: number) {
    const params = new URLSearchParams({ page: String(pageNum), page_size: String(pageSize) });
    if (sector) params.set('sector', sector);
    if (stage) params.set('stage', stage);
    if (area) params.set('area', area);
    if (q) params.set('q', q);
    if (hiringOnly) params.set('hiring', '1');
    return `${import.meta.env.VITE_API_URL}/api/companies?${params.toString()}`;
  }

  function fetchCompanies(pageNum: number, append: boolean, pageSize: number) {
    setLoading(true);
    fetch(buildURL(pageNum, pageSize), { credentials: 'include' })
      .then(r => (r.ok ? r.json() : (r.status === 401 && navigate('/auth'), null)))
      .then(d => {
        if (!d) return;
        setCompanies(prev => (append ? [...prev, ...(d.companies || [])] : d.companies || []));
        setTotal(d.total || 0);
        setOpenRoles(d.open_roles || 0);
        setFacets(d.facets || { field: [], level: [] });
      })
      .catch(console.error)
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    const timer = setTimeout(() => {
      setPage(1);
      // Map view isn't paginated — always pull the full matching set so it
      // never shows fewer companies than the grid does.
      fetchCompanies(1, false, view === 'map' ? MAP_PAGE_SIZE : PAGE_SIZE);
    }, 350);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sector, stage, area, q, view, hiringOnly]);

  const companiesWithPins = useMemo(
    () => companies.filter(c => c.lat != null && c.lng != null),
    [companies]
  );
  const pinPositions = useMemo(
    () => companiesWithPins.map(c => [c.lat as number, c.lng as number] as [number, number]),
    [companiesWithPins]
  );
  const canLoadMore = view === 'grid' && companies.length < total;

  return (
    <div className="p-6 md:p-8 max-w-7xl mx-auto space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="font-display text-2xl font-extrabold text-ink">Job Map</h1>
          <p className="text-sm text-ink-faint mt-1">
            {hiringOnly
              ? `${openRoles} open ${openRoles === 1 ? 'role' : 'roles'} across ${total} ${total === 1 ? 'company' : 'companies'}`
              : `${total} ${total === 1 ? 'company' : 'companies'} · ${openRoles} open ${openRoles === 1 ? 'role' : 'roles'}`}
          </p>
        </div>
        <div className="flex items-center gap-1 bg-paper border border-line rounded-full p-1">
          <button
            onClick={() => setView('grid')}
            className={`px-3 py-1.5 rounded-full text-xs font-semibold flex items-center gap-1.5 transition-colors ${view === 'grid' ? 'bg-ink text-white' : 'text-ink-soft hover:text-ink'}`}
          >
            <LayoutGrid className="w-3.5 h-3.5" /> Grid
          </button>
          <button
            onClick={() => setView('map')}
            className={`px-3 py-1.5 rounded-full text-xs font-semibold flex items-center gap-1.5 transition-colors ${view === 'map' ? 'bg-ink text-white' : 'text-ink-soft hover:text-ink'}`}
          >
            <MapViewIcon className="w-3.5 h-3.5" /> Map
          </button>
        </div>
      </div>

      <div className="flex flex-col sm:flex-row gap-3">
        <div className="relative flex-1">
          <Search className="absolute left-4 top-1/2 -translate-y-1/2 text-ink-faint w-4 h-4" />
          <input
            type="text"
            value={q}
            onChange={e => setQ(e.target.value)}
            placeholder="Search companies..."
            className="w-full bg-surface border border-line rounded-full py-2 pl-10 pr-4 text-sm text-ink font-mono focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent transition-all"
          />
        </div>
        <select
          value={sector}
          onChange={e => setSector(e.target.value)}
          className="bg-surface border border-line rounded-full py-2 px-4 text-sm text-ink focus:outline-none focus:border-accent"
        >
          <option value="">All sectors</option>
          {SECTORS.map(s => <option key={s} value={s}>{s}</option>)}
        </select>
        <select
          value={stage}
          onChange={e => setStage(e.target.value)}
          className="bg-surface border border-line rounded-full py-2 px-4 text-sm text-ink focus:outline-none focus:border-accent"
        >
          <option value="">All stages</option>
          {STAGES.map(s => <option key={s} value={s}>{s}</option>)}
        </select>
        <input
          type="text"
          value={area}
          onChange={e => setArea(e.target.value)}
          placeholder="Area / city"
          className="bg-surface border border-line rounded-full py-2 px-4 text-sm text-ink font-mono w-40 focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent"
        />
        <button
          onClick={() => setHiringOnly(!hiringOnly)}
          title={hiringOnly ? 'Showing only companies with open roles' : 'Showing every company in the directory'}
          className={`px-4 py-2 rounded-full text-sm font-semibold whitespace-nowrap transition-colors flex items-center gap-2 ${
            hiringOnly
              ? 'bg-accent text-white'
              : 'bg-surface border border-line text-ink-soft hover:text-ink'
          }`}
        >
          <Briefcase className="w-3.5 h-3.5" />
          {hiringOnly ? 'Hiring only' : 'All companies'}
        </button>
      </div>

      {/* Role facets — derived from job titles, so they narrow the roles
          shown inside each company rather than the company list itself. */}
      {(facets.field.length > 0 || facets.level.length > 0) && (
        <div className="space-y-2">
          {([
            { label: 'FIELD', items: facets.field, value: field, set: setField },
            { label: 'LEVEL', items: facets.level, value: level, set: setLevel },
          ] as const).map(row =>
            row.items.length === 0 ? null : (
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
                      className={`px-2.5 py-1 rounded-full text-xs transition-colors border ${
                        active
                          ? 'bg-ink text-white border-ink font-semibold'
                          : 'bg-surface border-line text-ink-soft hover:text-ink hover:border-line-strong'
                      }`}
                    >
                      {f.name} <span className={active ? 'text-white/70' : 'text-ink-faint'}>{f.count}</span>
                    </button>
                  );
                })}
              </div>
            )
          )}
        </div>
      )}

      {view === 'map' ? (
        <div className="rounded-xl border border-line overflow-hidden h-[560px]">
          <MapContainer center={[12.9716, 77.5946]} zoom={5} style={{ height: '100%', width: '100%' }}>
            <TileLayer
              attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
              url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
            />
            <FitBounds positions={pinPositions} />
            <MarkerClusterGroup chunkedLoading spiderfyOnMaxZoom>
              {companiesWithPins.map(c => (
                <Marker key={c.id} position={[c.lat as number, c.lng as number]} icon={pinIcon}>
                  <Popup minWidth={260}>
                    <div className="flex items-center gap-2 mb-1">
                      <CompanyLogo domain={c.domain} name={c.name} />
                      <div className="min-w-0">
                        <span className="font-semibold text-sm block truncate">{c.name}</span>
                        <span className="text-[10px] text-ink-faint">{c.area}</span>
                      </div>
                    </div>
                    <p className="text-xs text-ink-soft">{c.description}</p>

                    {c.job_count > 0 && (
                      <div className="mt-2 border-t border-line pt-1">
                        <p className="text-[10px] font-semibold text-ink uppercase tracking-wide mb-1">
                          {c.job_count} open {c.job_count === 1 ? 'role' : 'roles'}
                        </p>
                        <JobList companyId={c.id} field={field} level={level} />
                      </div>
                    )}

                    <div className="flex items-center gap-3 mt-2">
                      {c.website && (
                        <a href={c.website} target="_blank" rel="noreferrer" className="text-xs text-accent font-semibold">
                          Visit website
                        </a>
                      )}
                      {c.job_count === 0 && c.careers_url && (
                        <a href={c.careers_url} target="_blank" rel="noreferrer" className="text-xs text-accent font-semibold">
                          Careers page
                        </a>
                      )}
                    </div>
                  </Popup>
                </Marker>
              ))}
            </MarkerClusterGroup>
          </MapContainer>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {loading && companies.length === 0 ? (
            <div className="col-span-full p-8 text-center text-sm text-ink-faint">Loading companies...</div>
          ) : companies.length === 0 ? (
            <div className="col-span-full p-8 text-center text-sm text-ink-faint">No companies found yet — the discovery pipeline runs automatically every few hours.</div>
          ) : (
            companies.map(c => (
              <div key={c.id} className="bg-surface border border-line rounded-xl p-4 flex flex-col gap-3 hover:bg-paper/60 transition-colors">
                <div className="flex items-start gap-3">
                  <CompanyLogo domain={c.domain} name={c.name} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <h3 className="font-semibold text-sm text-ink truncate">{c.name}</h3>
                      {c.job_count > 0 && (
                        <span className="px-1.5 py-0.5 rounded-full text-[10px] font-semibold bg-accent text-white flex-shrink-0">
                          {c.job_count} open
                        </span>
                      )}
                    </div>
                    <p className="text-xs text-ink-faint truncate">{c.area}</p>
                  </div>
                </div>
                <p className="text-xs text-ink-soft line-clamp-3 flex-1">{c.description}</p>
                <div className="flex items-center gap-2 flex-wrap">
                  {c.sector && <span className="px-2 py-0.5 rounded-full text-[10px] font-semibold bg-accent-soft text-accent">{c.sector}</span>}
                  {c.stage && <span className="px-2 py-0.5 rounded-full text-[10px] font-semibold bg-info-soft text-info">{c.stage}</span>}
                </div>

                {expandedId === c.id && (
                  <div className="border-t border-line pt-2">
                    <JobList companyId={c.id} field={field} level={level} />
                  </div>
                )}

                <div className="flex items-center gap-2 pt-1">
                  {c.website && (
                    <a href={c.website} target="_blank" rel="noreferrer" className="text-xs font-semibold text-ink-soft hover:text-ink">
                      Website
                    </a>
                  )}
                  {c.job_count > 0 ? (
                    <button
                      onClick={() => setExpandedId(expandedId === c.id ? null : c.id)}
                      className="ml-auto text-xs font-semibold px-3 py-1.5 rounded-full bg-ink text-white hover:bg-black flex items-center gap-1.5"
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
        <div className="flex justify-center">
          <button
            onClick={() => {
              const next = page + 1;
              setPage(next);
              fetchCompanies(next, true, PAGE_SIZE);
            }}
            disabled={loading}
            className="text-sm font-semibold px-5 py-2 rounded-full border border-line-strong text-ink hover:bg-paper transition-colors disabled:opacity-50"
          >
            {loading ? 'Loading...' : 'Load more'}
          </button>
        </div>
      )}
    </div>
  );
}
