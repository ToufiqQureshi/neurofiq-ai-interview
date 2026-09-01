import { useEffect, useMemo, useState } from 'react';
import { MapContainer, TileLayer, Marker, Popup, useMap } from 'react-leaflet';
import MarkerClusterGroup from 'react-leaflet-cluster';
import L from 'leaflet';
import 'leaflet/dist/leaflet.css';
import 'leaflet.markercluster/dist/MarkerCluster.css';
import 'leaflet.markercluster/dist/MarkerCluster.Default.css';
import { ExternalLink, Sparkles, MapPin, Briefcase, ChevronDown } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import CompanyJobList from './CompanyJobList';
import CompanyDrawer from './CompanyDrawer';

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

interface LeafletCompanyMapProps {
  companies: Company[];
  selectedHub: TechHub;
}

function createCompanyLogoIcon(company: Company) {
  const faviconUrl = company.domain ? `https://www.google.com/s2/favicons?domain=${company.domain}&sz=128` : '';
  const initial = company.name ? company.name.charAt(0).toUpperCase() : 'C';
  const hasJobs = company.job_count > 0;
  const badgeHtml = hasJobs
    ? `<span class="company-pin-badge">${company.job_count}</span>`
    : '';

  const html = `
    <div class="company-map-pin ${hasJobs ? 'hiring' : ''}" title="${company.name} (${company.area})">
      <div class="company-pin-avatar">
        ${
          faviconUrl
            ? `<img src="${faviconUrl}" alt="${company.name}" onerror="this.style.display='none';if(this.nextElementSibling)this.nextElementSibling.style.display='flex';" />
               <span class="company-pin-fallback" style="display:none;">${initial}</span>`
            : `<span class="company-pin-fallback">${initial}</span>`
        }
      </div>
      ${badgeHtml}
    </div>
  `;

  return L.divIcon({
    className: 'custom-company-pin-container',
    html: html,
    iconSize: [38, 38],
    iconAnchor: [19, 19],
    popupAnchor: [0, -22],
  });
}

function HubMapController({ selectedHub }: { selectedHub: TechHub }) {
  const map = useMap();

  useEffect(() => {
    map.setMinZoom(selectedHub.minZoom);
    map.setMaxZoom(selectedHub.maxZoom);

    // In Leaflet, bounds format is [[southLat, westLng], [northLat, eastLng]]
    const southLat = selectedHub.bounds[0][1];
    const westLng = selectedHub.bounds[0][0];
    const northLat = selectedHub.bounds[1][1];
    const eastLng = selectedHub.bounds[1][0];

    const bounds = L.latLngBounds([southLat, westLng], [northLat, eastLng]);
    map.setMaxBounds(bounds);
    map.options.maxBoundsViscosity = 1.0;

    map.flyTo([selectedHub.lat, selectedHub.lng], selectedHub.zoom, {
      duration: 0.8,
      easeLinearity: 0.25,
    });
  }, [selectedHub, map]);

  return null;
}

function CompanyPopupContent({ c }: { c: Company }) {
  const [showRoles, setShowRoles] = useState(false);
  const navigate = useNavigate();

  return (
    <div className="p-1 font-sans">
      <div className="flex items-center gap-2.5 mb-2">
        <div className="w-8 h-8 rounded-lg bg-slate-100 border border-slate-200 flex items-center justify-center overflow-hidden shrink-0">
          {c.domain ? (
            <img
              src={`https://www.google.com/s2/favicons?domain=${c.domain}&sz=128`}
              alt={c.name}
              className="w-6 h-6 object-contain"
              onError={e => {
                (e.currentTarget as HTMLElement).style.display = 'none';
              }}
            />
          ) : (
            <span className="font-bold text-xs text-slate-700">{c.name.charAt(0)}</span>
          )}
        </div>
        <div className="min-w-0 flex-1">
          <span className="font-bold text-sm text-ink block truncate">{c.name}</span>
          <span className="text-[10px] text-ink-faint flex items-center gap-1 font-mono">
            <MapPin className="w-2.5 h-2.5 text-accent" /> {c.area}
          </span>
        </div>
      </div>
      <p className="text-xs text-ink-soft leading-relaxed mb-2 line-clamp-2">{c.description}</p>

      <div className="flex items-center gap-1.5 flex-wrap mb-2.5">
        {c.sector && (
          <span className="px-2 py-0.5 rounded-full text-[10px] font-semibold bg-accent-soft text-accent">
            {c.sector}
          </span>
        )}
        {c.stage && (
          <span className="px-2 py-0.5 rounded-full text-[10px] font-semibold bg-info-soft text-info">
            {c.stage}
          </span>
        )}
        {c.job_count > 0 && (
          <button
            onClick={() => setShowRoles(!showRoles)}
            className="px-2 py-0.5 rounded-full text-[10px] font-semibold bg-emerald-50 text-emerald-700 hover:bg-emerald-100 transition-colors font-mono flex items-center gap-1 cursor-pointer border border-emerald-200/80 shadow-sm"
          >
            <Briefcase className="w-2.5 h-2.5" />
            {showRoles ? 'Hide roles' : `${c.job_count} open ${c.job_count === 1 ? 'role' : 'roles'}`}
            <ChevronDown className={`w-2.5 h-2.5 transition-transform ${showRoles ? 'rotate-180' : ''}`} />
          </button>
        )}
      </div>

      {showRoles && (
        <div className="mb-2.5 pt-2 border-t border-slate-100">
          <CompanyJobList companyId={c.id} companyName={c.name} />
        </div>
      )}

      <div className="flex items-center justify-between gap-2 pt-2 border-t border-line">
        {c.website ? (
          <a
            href={c.website}
            target="_blank"
            rel="noreferrer"
            className="text-xs text-accent font-semibold hover:underline flex items-center gap-1"
          >
            Website <ExternalLink className="w-2.5 h-2.5" />
          </a>
        ) : <span />}
        <button
          onClick={() => navigate('/dashboard')}
          className="px-3 py-1.5 rounded-xl text-[11px] font-semibold bg-ink text-white hover:bg-black transition-colors flex items-center gap-1 shadow-sm"
        >
          <Sparkles className="w-3 h-3 text-amber-300" /> Practice Mock
        </button>
      </div>
    </div>
  );
}

export default function LeafletCompanyMap({ companies, selectedHub }: LeafletCompanyMapProps) {
  const [selectedCompany, setSelectedCompany] = useState<Company | null>(null);

  const companiesWithPins = useMemo(
    () => companies.filter(c => typeof c.lat === 'number' && typeof c.lng === 'number'),
    [companies]
  );

  return (
    <div className="rounded-2xl border border-line overflow-hidden h-[calc(100vh-210px)] min-h-[600px] shadow-xl relative bg-white">
      <MapContainer
        center={[selectedHub.lat, selectedHub.lng]}
        zoom={selectedHub.zoom}
        style={{ height: '100%', width: '100%' }}
      >
        <TileLayer
          attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
          url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
          maxZoom={19}
        />
        <HubMapController selectedHub={selectedHub} />
        <MarkerClusterGroup
          chunkedLoading
          spiderfyOnMaxZoom
          showCoverageOnHover={false}
          maxClusterRadius={25}
          disableClusteringAtZoom={11}
        >
          {companiesWithPins.map(c => (
            <Marker
              key={c.id}
              position={[c.lat as number, c.lng as number]}
              icon={createCompanyLogoIcon(c)}
              eventHandlers={{
                click: () => {
                  setSelectedCompany(c);
                },
              }}
            >
              <Popup minWidth={280} maxWidth={340}>
                <CompanyPopupContent c={c} />
              </Popup>
            </Marker>
          ))}
        </MarkerClusterGroup>
      </MapContainer>

      {/* Floating Company Detail Drawer (Inspector Card) */}
      <CompanyDrawer
        company={selectedCompany}
        onClose={() => setSelectedCompany(null)}
      />

      {/* Active Tech Hub Watermark Pill */}
      <div className="absolute bottom-4 left-4 z-[400] bg-white/95 backdrop-blur-md px-3.5 py-2 rounded-full border border-slate-200 shadow-md flex items-center gap-2">
        <span className="text-sm">{selectedHub.icon}</span>
        <span className="text-xs font-bold text-slate-800">{selectedHub.name}</span>
        <span className="text-[10px] font-mono text-slate-500 border-l border-slate-200 pl-2">
          {companiesWithPins.length} Startups Plotted (2D View)
        </span>
      </div>
    </div>
  );
}
