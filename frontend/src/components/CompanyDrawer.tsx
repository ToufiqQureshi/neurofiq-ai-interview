import { X, ExternalLink, Sparkles, MapPin, Briefcase } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import CompanyJobList from './CompanyJobList';

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

interface CompanyDrawerProps {
  company: Company | null;
  onClose: () => void;
}

export default function CompanyDrawer({ company, onClose }: CompanyDrawerProps) {
  const navigate = useNavigate();

  if (!company) return null;

  const faviconUrl = company.domain
    ? `https://www.google.com/s2/favicons?domain=${company.domain}&sz=128`
    : '';

  return (
    <div className="absolute top-4 right-4 z-[500] w-84 sm:w-96 max-h-[calc(100%-2rem)] bg-white/95 backdrop-blur-xl border border-slate-200/90 rounded-2xl shadow-2xl flex flex-col overflow-hidden animate-fade-in transition-all">
      {/* Drawer Header */}
      <div className="p-4 border-b border-slate-100 flex items-start justify-between gap-3 bg-gradient-to-b from-slate-50/80 to-transparent">
        <div className="flex items-center gap-3 min-w-0">
          <div className="w-12 h-12 rounded-xl bg-white border border-slate-200 shadow-sm flex items-center justify-center overflow-hidden shrink-0 p-1.5">
            {faviconUrl ? (
              <img
                src={faviconUrl}
                alt={company.name}
                className="w-full h-full object-contain"
                onError={e => {
                  (e.currentTarget as HTMLElement).style.display = 'none';
                }}
              />
            ) : (
              <span className="font-bold text-lg text-slate-800 font-mono">
                {company.name.charAt(0)}
              </span>
            )}
          </div>
          <div className="min-w-0 flex-1">
            <h3 className="font-bold text-base text-slate-900 truncate tracking-tight">
              {company.name}
            </h3>
            <p className="text-xs text-slate-500 flex items-center gap-1 font-mono mt-0.5 truncate">
              <MapPin className="w-3 h-3 text-indigo-500 shrink-0" /> {company.area}
            </p>
          </div>
        </div>
        <button
          onClick={onClose}
          className="p-1.5 rounded-full text-slate-400 hover:text-slate-700 hover:bg-slate-100 transition-colors"
          title="Close details"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* Drawer Scrollable Content */}
      <div className="p-4 overflow-y-auto space-y-4 flex-1">
        {/* Tags */}
        <div className="flex items-center gap-1.5 flex-wrap">
          {company.sector && (
            <span className="px-2.5 py-1 rounded-full text-xs font-semibold bg-indigo-50 text-indigo-700 border border-indigo-100 font-mono">
              {company.sector}
            </span>
          )}
          {company.stage && (
            <span className="px-2.5 py-1 rounded-full text-xs font-semibold bg-sky-50 text-sky-700 border border-sky-100 font-mono">
              {company.stage}
            </span>
          )}
          {company.job_count > 0 ? (
            <span className="px-2.5 py-1 rounded-full text-xs font-semibold bg-emerald-50 text-emerald-700 border border-emerald-200 font-mono flex items-center gap-1">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-ping" />
              {company.job_count} Hiring Now
            </span>
          ) : (
            <span className="px-2.5 py-1 rounded-full text-xs font-medium bg-slate-100 text-slate-500 font-mono">
              Directory Listed
            </span>
          )}
        </div>

        {/* Description */}
        <p className="text-xs text-slate-600 leading-relaxed bg-slate-50/80 p-3 rounded-xl border border-slate-100">
          {company.description || 'Indian tech startup listed in the NeuroFIQ ecosystem.'}
        </p>

        {/* Action Links */}
        <div className="flex items-center gap-2">
          {company.website && (
            <a
              href={company.website}
              target="_blank"
              rel="noreferrer"
              className="flex-1 px-3 py-2 rounded-xl text-xs font-semibold bg-slate-100 hover:bg-slate-200 text-slate-800 transition-colors flex items-center justify-center gap-1.5 shadow-sm"
            >
              Company Website <ExternalLink className="w-3 h-3 text-slate-500" />
            </a>
          )}
          {company.careers_url && (
            <a
              href={company.careers_url}
              target="_blank"
              rel="noreferrer"
              className="flex-1 px-3 py-2 rounded-xl text-xs font-semibold bg-slate-100 hover:bg-slate-200 text-slate-800 transition-colors flex items-center justify-center gap-1.5 shadow-sm"
            >
              Careers Page <ExternalLink className="w-3 h-3 text-slate-500" />
            </a>
          )}
        </div>

        {/* Live Open Roles Header */}
        <div className="pt-2 border-t border-slate-100">
          <div className="flex items-center justify-between mb-2">
            <h4 className="text-xs font-bold text-slate-900 uppercase tracking-wider flex items-center gap-1.5">
              <Briefcase className="w-3.5 h-3.5 text-indigo-600" /> Active Job Openings ({company.job_count})
            </h4>
          </div>

          <CompanyJobList companyId={company.id} companyName={company.name} />
        </div>
      </div>

      {/* Drawer Footer CTA */}
      <div className="p-3 border-t border-slate-100 bg-slate-50/90 flex items-center gap-2">
        <button
          onClick={() => navigate(`/repositories?company=${encodeURIComponent(company.id)}`)}
          className="w-full py-2.5 px-4 rounded-xl text-xs font-bold bg-slate-900 hover:bg-black text-white transition-all flex items-center justify-center gap-2 shadow-md"
        >
          <Sparkles className="w-4 h-4 text-amber-300" /> Practice Mock for {company.name}
        </button>
      </div>
    </div>
  );
}
