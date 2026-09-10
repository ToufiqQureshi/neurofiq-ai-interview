import React from 'react';
import { Sparkles, ExternalLink, MapPin, Building2, Clock, CheckCircle2 } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import CompanyLogo from './CompanyLogo';

export interface JobCardData {
  id: string;
  title: string;
  companyName: string;
  companyDomain?: string;
  companyId?: string;
  location?: string;
  department?: string;
  url: string;
  atsName?: string;
  experienceLevel?: string;
  workType?: string;
  postedDate?: string;
}

interface JobListingCardProps {
  job: JobCardData;
  onSelectCompany?: (companyId: string) => void;
}

export function JobListingCard({
  job,
  onSelectCompany,
}: JobListingCardProps) {
  const navigate = useNavigate();


  const handlePracticeInterview = (e: React.MouseEvent) => {
    e.stopPropagation();
    // Navigate to interview setup or start session with tailored job context
    navigate(`/dashboard?practice_job=${encodeURIComponent(job.title)}&company=${encodeURIComponent(job.companyName)}`);
  };

  const atsDisplay = job.atsName || 'Verified ATS';

  return (
    <div className="group relative bg-paper dark:bg-zinc-900/90 border border-line hover:border-accent/40 rounded-2xl p-4 sm:p-5 transition-all shadow-xs hover:shadow-lg hover:-translate-y-0.5 flex flex-col justify-between gap-4">
      {/* Top Bar: Company Logo, Title & Bookmark */}
      <div className="flex items-start gap-3.5">
        {/* Company Logo */}
        <CompanyLogo
          domain={job.companyDomain}
          name={job.companyName}
          className="w-11 h-11 rounded-xl"
          fallbackClassName="bg-accent-soft border border-line text-accent"
          fallbackTextClassName="text-sm"
        />

        {/* Title, Company & Tags */}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 flex-wrap">
            <button
              type="button"
              onClick={() => job.companyId && onSelectCompany && onSelectCompany(job.companyId)}
              className="text-xs font-semibold text-ink-soft hover:text-accent transition-colors truncate max-w-[200px]"
            >
              {job.companyName}
            </button>
            <span className="text-[10px] font-mono px-2 py-0.5 rounded-full bg-zinc-100 dark:bg-zinc-800 text-ink-faint border border-line/60">
              {atsDisplay}
            </span>
          </div>

          <h3 className="text-sm sm:text-base font-bold text-ink group-hover:text-accent transition-colors line-clamp-2 mt-0.5">
            {job.title}
          </h3>

          {/* Location & Department */}
          <div className="flex items-center gap-2.5 mt-1.5 flex-wrap text-xs text-ink-faint">
            {job.location && (
              <span className="flex items-center gap-1 truncate">
                <MapPin className="w-3 h-3 text-accent flex-shrink-0" />
                <span className="truncate">{job.location}</span>
              </span>
            )}
            {job.department && (
              <span className="flex items-center gap-1 truncate">
                <Building2 className="w-3 h-3 flex-shrink-0" />
                <span className="truncate">{job.department}</span>
              </span>
            )}
            <span className="flex items-center gap-1 font-mono text-[11px] text-emerald-600 dark:text-emerald-400">
              <Clock className="w-3 h-3 flex-shrink-0" />
              {job.postedDate || 'Fresh 24h'}
            </span>
          </div>
        </div>

      </div>

      {/* Bottom Bar: Action Triggers */}
      <div className="flex items-center justify-between gap-3 pt-3 border-t border-line/60">
        <div className="flex items-center gap-1.5">
          <span className="inline-flex items-center gap-1 text-[11px] font-medium text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-950/40 px-2 py-0.5 rounded-md border border-emerald-500/20">
            <CheckCircle2 className="w-3 h-3" /> Verified Role
          </span>
        </div>

        <div className="flex items-center gap-2">
          {/* Practice AI Mock Interview Button (Our Superpower!) */}
          <button
            type="button"
            onClick={handlePracticeInterview}
            className="px-3 py-1.5 rounded-xl bg-accent-soft hover:bg-accent text-accent hover:text-white text-xs font-semibold transition-all flex items-center gap-1.5 shadow-xs"
            title="Start instant tailored AI Mock Interview for this role"
          >
            <Sparkles className="w-3.5 h-3.5" />
            <span>AI Mock Interview</span>
          </button>

          {/* External Apply Link */}
          <a
            href={job.url}
            target="_blank"
            rel="noopener noreferrer"
            onClick={e => e.stopPropagation()}
            className="p-1.5 rounded-xl text-ink-faint hover:text-ink hover:bg-zinc-100 dark:hover:bg-zinc-800 border border-line transition-colors flex-shrink-0"
            title="Open direct job application page"
          >
            <ExternalLink className="w-3.5 h-3.5" />
          </a>
        </div>
      </div>
    </div>
  );
}
