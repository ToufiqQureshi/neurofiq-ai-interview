import { useEffect, useRef, useState } from 'react';
import * as maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { useNavigate } from 'react-router-dom';
import { Sparkles, Compass } from 'lucide-react';
import CompanyDrawer from './CompanyDrawer';

// Every pin and popup below is built with innerHTML for MapLibre's marker
// API, and every value going into them — company name, description, job
// title, department — comes from a scraped ATS board or LLM extraction, not
// from this app. A job title containing a stray `<` is all it takes to break
// out of the template. Escape on the way in rather than trusting the source.
function escapeHtml(value: unknown): string {
  return String(value ?? '').replace(/[&<>"']/g, (ch) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch] as string
  ));
}

// Scraped URLs are only ever used as href/src here, never executed — but a
// javascript: or data: URL in an href is a click away from running. Only
// http(s) is a legitimate destination for a company site or job posting.
function safeUrl(value: unknown): string {
  const s = String(value ?? '').trim();
  return /^https?:\/\//i.test(s) ? s : '';
}

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

interface MapLibreCompanyMapProps {
  companies: Company[];
  selectedHub: TechHub;
  onSelectHub?: (hub: TechHub) => void;
}

export default function MapLibreCompanyMap({ companies, selectedHub }: MapLibreCompanyMapProps) {
  const mapContainerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const markersRef = useRef<maplibregl.Marker[]>([]);
  const navigate = useNavigate();
  const [selectedCompany, setSelectedCompany] = useState<Company | null>(null);
  const [isStyleLoaded, setIsStyleLoaded] = useState(false);

  // 1. Initialize the 3D MapLibre GL WebGL Map Instance
  useEffect(() => {
    if (!mapContainerRef.current) return;

    const maplibreStyle: maplibregl.StyleSpecification = {
      version: 8,
      sources: {
        'osm-tiles': {
          type: 'raster',
          tiles: [
            'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
          ],
          tileSize: 256,
          attribution: '&copy; OpenStreetMap contributors',
        },
      },
      layers: [
        {
          id: 'osm-layer',
          type: 'raster',
          source: 'osm-tiles',
          minzoom: 0,
          maxzoom: 19,
        },
      ],
    };

    const map = new maplibregl.Map({
      container: mapContainerRef.current,
      style: maplibreStyle,
      center: [selectedHub.lng, selectedHub.lat],
      zoom: selectedHub.zoom,
      pitch: 50, // 3D Camera tilt angle
      bearing: -12, // Perspective rotation
      maxBounds: selectedHub.bounds,
      minZoom: selectedHub.minZoom,
      maxZoom: selectedHub.maxZoom,
      attributionControl: false,
    });

    // Add navigation and compass controls
    map.addControl(new maplibregl.NavigationControl({ visualizePitch: true }), 'top-right');
    map.addControl(new maplibregl.AttributionControl({ compact: true }), 'bottom-right');

    map.on('load', () => {
      setIsStyleLoaded(true);
    });

    mapRef.current = map;

    return () => {
      map.remove();
      mapRef.current = null;
    };
  }, []);

  // 2. Fly smoothly to selected Tech Hub and update bounds/zoom constraints
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;

    // Apply city boundary locks and zoom clamps
    map.setMinZoom(selectedHub.minZoom);
    map.setMaxZoom(selectedHub.maxZoom);
    map.setMaxBounds(selectedHub.bounds);

    map.flyTo({
      center: [selectedHub.lng, selectedHub.lat],
      zoom: selectedHub.zoom,
      pitch: selectedHub.id === 'all' ? 25 : 55,
      bearing: selectedHub.id === 'all' ? 0 : -15,
      speed: 1.2,
      curve: 1.4,
      essential: true,
    });
  }, [selectedHub]);

  // 3. Render Custom 3D Logo Pins on the Map
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;

    // Remove existing markers
    markersRef.current.forEach((m) => m.remove());
    markersRef.current = [];

    const companiesWithPins = companies.filter(
      (c) => typeof c.lat === 'number' && typeof c.lng === 'number'
    );

    companiesWithPins.forEach((c) => {
      const el = document.createElement('div');
      el.className = 'custom-maplibre-pin';
      
      const faviconUrl = c.domain
        ? `https://www.google.com/s2/favicons?domain=${encodeURIComponent(c.domain)}&sz=128`
        : '';
      const safeName = escapeHtml(c.name);
      const initial = escapeHtml(c.name ? c.name.charAt(0).toUpperCase() : 'C');
      const hasJobs = c.job_count > 0;

      el.innerHTML = `
        <div class="maplibre-pin-card ${hasJobs ? 'hiring' : ''}">
          <div class="maplibre-pin-avatar">
            ${
              faviconUrl
                ? `<img src="${escapeHtml(faviconUrl)}" alt="${safeName}" onerror="this.style.display='none';this.nextElementSibling.style.display='flex';" />
                   <span class="maplibre-pin-fallback" style="display:none;">${initial}</span>`
                : `<span class="maplibre-pin-fallback">${initial}</span>`
            }
          </div>
          ${hasJobs ? `<span class="maplibre-pin-badge">${escapeHtml(c.job_count)}</span>` : ''}
        </div>
      `;

      // Interactive Popup
      const popupHtml = `
        <div class="p-3 font-sans max-w-[280px]">
          <div class="flex items-center gap-2.5 mb-2">
            <div class="w-8 h-8 rounded-lg bg-slate-100 border border-slate-200 flex items-center justify-center overflow-hidden shrink-0">
              ${
                faviconUrl
                  ? `<img src="${escapeHtml(faviconUrl)}" alt="${safeName}" class="w-6 h-6 object-contain" />`
                  : `<span class="font-bold text-xs text-slate-700">${initial}</span>`
              }
            </div>
            <div class="min-w-0 flex-1">
              <h4 class="font-bold text-sm text-slate-900 leading-tight truncate">${safeName}</h4>
              <span class="text-[11px] text-slate-500 flex items-center gap-1 font-mono mt-0.5">
                📍 ${escapeHtml(c.area)}
              </span>
            </div>
          </div>
          <p class="text-xs text-slate-600 leading-relaxed mb-3 line-clamp-2">${escapeHtml(c.description) || 'Indian tech startup.'}</p>
          <div class="flex items-center gap-1.5 flex-wrap mb-3">
            <span class="text-[10px] font-mono px-2 py-0.5 rounded-full bg-slate-100 text-slate-700 font-semibold border border-slate-200">${escapeHtml(c.sector) || 'Tech'}</span>
            <span class="text-[10px] font-mono px-2 py-0.5 rounded-full bg-slate-100 text-slate-700 font-semibold border border-slate-200">${escapeHtml(c.stage) || 'Startup'}</span>
            ${
              hasJobs
                ? `<button id="map-roles-toggle-${c.id}" class="text-[10px] font-mono px-2.5 py-0.5 rounded-full bg-emerald-50 text-emerald-700 hover:bg-emerald-100 font-semibold border border-emerald-200/80 cursor-pointer transition-colors flex items-center gap-1 shadow-sm">💼 ${c.job_count} open role${c.job_count > 1 ? 's' : ''} ▼</button>`
                : ''
            }
          </div>

          <div id="map-roles-container-${c.id}" style="display:none;" class="my-2 max-h-48 overflow-y-auto divide-y divide-slate-100 border-t border-b border-slate-100 py-1 font-sans">
            <p class="text-[11px] text-slate-400 text-center py-2 animate-pulse">Loading live roles…</p>
          </div>

          <div class="flex items-center justify-between gap-2 pt-2 border-t border-slate-100">
            ${
              safeUrl(c.website)
                ? `<a href="${escapeHtml(safeUrl(c.website))}" target="_blank" rel="noopener noreferrer" class="text-xs text-indigo-600 font-semibold hover:underline flex items-center gap-1">Website ↗</a>`
                : '<span></span>'
            }
            <button id="map-practice-btn-${c.id}" class="px-3 py-1.5 rounded-xl bg-slate-900 hover:bg-black text-white text-[11px] font-semibold shadow-md transition-all flex items-center gap-1">
              🎯 Practice Mock
            </button>
          </div>
        </div>
      `;

      const popup = new maplibregl.Popup({
        offset: 20,
        closeButton: true,
        closeOnClick: false,
        className: 'custom-maplibre-popup',
        maxWidth: '320px',
      }).setHTML(popupHtml);

      popup.on('open', () => {
        const practiceBtn = document.getElementById(`map-practice-btn-${c.id}`);
        if (practiceBtn) {
          practiceBtn.onclick = () => {
            navigate(`/repositories?company=${encodeURIComponent(c.id)}`);
          };
        }

        const rolesToggle = document.getElementById(`map-roles-toggle-${c.id}`);
        const rolesContainer = document.getElementById(`map-roles-container-${c.id}`);

        if (rolesToggle && rolesContainer) {
          rolesToggle.onclick = async () => {
            if (rolesContainer.style.display === 'block') {
              rolesContainer.style.display = 'none';
              rolesToggle.innerHTML = `💼 ${c.job_count} open role${c.job_count > 1 ? 's' : ''} ▼`;
            } else {
              rolesContainer.style.display = 'block';
              rolesToggle.innerHTML = `💼 Hide roles ▲`;

              try {
                const res = await fetch(`${import.meta.env.VITE_API_URL}/api/companies/${c.id}/jobs`, { credentials: 'include' });
                const data = await res.json();
                const jobs = data.jobs || [];

                if (jobs.length === 0) {
                  rolesContainer.innerHTML = `<p class="text-[11px] text-slate-400 text-center py-2">No active postings right now.</p>`;
                } else {
                  rolesContainer.innerHTML = jobs
                    .map(
                      (j: { id: string; title: string; department: string; location: string; url: string }) => `
                      <div class="py-2 px-1 hover:bg-slate-50 transition-colors flex items-start justify-between gap-1.5">
                        <div class="min-w-0 flex-1">
                          <p class="text-xs font-semibold text-slate-800 truncate leading-tight">${escapeHtml(j.title)}</p>
                          <p class="text-[10px] text-slate-400 font-mono truncate mt-0.5">${escapeHtml([j.department, j.location].filter(Boolean).join(' · '))}</p>
                        </div>
                        <div class="flex items-center gap-1 shrink-0">
                          <button id="job-mock-btn-${j.id}" class="px-2 py-0.5 rounded-md text-[10px] font-semibold bg-indigo-50 text-indigo-700 hover:bg-indigo-600 hover:text-white transition-all shadow-sm">Mock</button>
                          ${safeUrl(j.url) ? `<a href="${escapeHtml(safeUrl(j.url))}" target="_blank" rel="noopener noreferrer" class="text-slate-400 hover:text-slate-700 p-1 text-xs">↗</a>` : ''}
                        </div>
                      </div>
                    `
                    )
                    .join('');

                  // Wire up mock interview click handlers
                  jobs.forEach((j: { id: string }) => {
                    const btn = document.getElementById(`job-mock-btn-${j.id}`);
                    if (btn) {
                      btn.onclick = (e) => {
                        e.stopPropagation();
                        navigate(`/repositories?job=${encodeURIComponent(j.id)}`);
                      };
                    }
                  });
                }
              } catch {
                rolesContainer.innerHTML = `<p class="text-[11px] text-red-500 text-center py-2">Failed to load roles.</p>`;
              }
            }
          };
        }
      });

      el.addEventListener('click', () => {
        setSelectedCompany(c);
      });

      const marker = new maplibregl.Marker({ element: el, anchor: 'center' })
        .setLngLat([c.lng as number, c.lat as number])
        .setPopup(popup)
        .addTo(map);

      markersRef.current.push(marker);
    });
  }, [companies, navigate, isStyleLoaded]);

  const resetNorth = () => {
    const map = mapRef.current;
    if (!map) return;
    map.easeTo({
      bearing: 0,
      pitch: 55,
      duration: 600,
    });
  };

  return (
    <div className="relative w-full h-[calc(100vh-210px)] min-h-[600px] rounded-2xl overflow-hidden border border-line shadow-xl bg-slate-900">
      {/* WebGL Canvas Container */}
      <div ref={mapContainerRef} className="w-full h-full" />

      {/* Floating 3D Quick Controls */}
      <div className="absolute top-4 left-4 z-10 flex items-center gap-2 bg-white/90 backdrop-blur-md px-3 py-2 rounded-2xl border border-slate-200/80 shadow-lg">
        <span className="flex items-center gap-1.5 text-xs font-bold text-slate-800 tracking-tight">
          <Sparkles className="w-3.5 h-3.5 text-amber-500" /> 3D WebGL
        </span>
        <div className="h-4 w-px bg-slate-200" />
        <button
          onClick={resetNorth}
          title="Reset Camera to North"
          className="flex items-center gap-1 text-[11px] font-semibold text-slate-700 hover:text-black px-2 py-1 rounded-lg hover:bg-slate-100 transition-colors"
        >
          <Compass className="w-3 h-3 text-accent" /> Reset Compass
        </button>
      </div>

      {/* Floating Company Detail Drawer (Inspector Card) */}
      <CompanyDrawer
        company={selectedCompany}
        onClose={() => setSelectedCompany(null)}
      />

      {/* Active Tech Hub Watermark Pill */}
      <div className="absolute bottom-4 left-4 z-10 bg-white/95 backdrop-blur-md px-3.5 py-2 rounded-full border border-slate-200 shadow-md flex items-center gap-2">
        <span className="text-sm">{selectedHub.icon}</span>
        <span className="text-xs font-bold text-slate-800">{selectedHub.name}</span>
        <span className="text-[10px] font-mono text-slate-500 border-l border-slate-200 pl-2">
          {companies.filter((c) => c.lat && c.lng).length} Startups Plotted
        </span>
      </div>
    </div>
  );
}
