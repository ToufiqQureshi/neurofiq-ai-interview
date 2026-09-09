import { useEffect, useRef, useState } from 'react';
import * as maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { Sparkles, Compass } from 'lucide-react';
import CompanyDrawer from './CompanyDrawer';
import type { Company, TechHub } from '../lib/types';

// Each pin below is built with innerHTML, because that is what MapLibre's
// marker API takes, and the company name going into it comes from a scraped
// ATS board or an LLM extraction rather than from this app. A name containing
// a stray `<` is all it takes to break out of the template. Escape on the way
// in rather than trusting the source.
function escapeHtml(value: unknown): string {
  return String(value ?? '').replace(/[&<>"']/g, (ch) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch] as string
  ));
}



interface MapLibreCompanyMapProps {
  companies: Company[];
  selectedHub: TechHub;
}

export default function MapLibreCompanyMap({ companies, selectedHub }: MapLibreCompanyMapProps) {
  const mapContainerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const markersRef = useRef<maplibregl.Marker[]>([]);
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

      // A pin opens the drawer, and only the drawer. There used to be a popup
      // as well, opening at the same time on the same click: 115 lines of
      // innerHTML that rendered the company's logo, name, area, description,
      // sector, stage, website link and roles list — everything CompanyDrawer
      // was already rendering behind it, down to a second fetch of the same
      // /jobs endpoint CompanyJobList calls. Two views of one company, kept in
      // step by hand, one of them assembled from strings and reachable only
      // through getElementById.
      el.addEventListener('click', () => {
        setSelectedCompany(c);
      });

      const marker = new maplibregl.Marker({ element: el, anchor: 'center' })
        .setLngLat([c.lng as number, c.lat as number])
        .addTo(map);

      markersRef.current.push(marker);
    });
  }, [companies, isStyleLoaded]);

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
