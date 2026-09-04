import React, { useState } from 'react';
import { Sparkles, ExternalLink, Bookmark, MapPin, Building2, Clock, CheckCircle2 } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

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
  isBookmarked?: boolean;
}

interface JobListingCardProps {
  job: JobCardData;
  onBookmarkToggle?: (jobId: string) => void;
  onSelectCompany?: (companyId: string) => void;
}

export function JobListingCard({
  job,
  onBookmarkToggle,
  onSelectCompany,
}: JobListingCardProps) {
  const navigate = useNavigate();
  const [logoFailed, setLogoFailed] = useState(false);
  const [bookmarked, setBookmarked] = useState(job.isBookmarked || false);

  const handleBookmark = (e: React.MouseEvent) => {
    e.stopPropagation();
    setBookmarked(!bookmarked);
    if (onBookmarkToggle) onBookmarkToggle(job.id);
  };

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
        {job.companyDomain && !logoFailed ? (
          <img
            src={`https://www.google.com/s2/favicons?domain=${job.companyDomain}&sz=128`}
            alt={job.companyName}
            className="w-11 h-11 rounded-xl border border-line object-contain bg-white flex-shrink-0 p-1 shadow-xs"
            onError={() => setLogoFailed(true)}
          />
        ) : (
          <div className="w-11 h-11 rounded-xl bg-accent-soft border border-line flex items-center justify-center text-accent text-sm font-bold uppercase flex-shrink-0 font-mono">
            {job.companyName.slice(0, 2)}
          </div>
        )}

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

        {/* Bookmark Button */}
        <button
          type="button"
          onClick={handleBookmark}
          title={bookmarked ? 'Saved' : 'Save job'}
          className={`p-1.5 rounded-xl border transition-all flex-shrink-0 ${
            bookmarked
              ? 'bg-accent-soft text-accent border-accent/40'
              : 'text-ink-faint border-transparent hover:border-line hover:text-ink hover:bg-zinc-100 dark:hover:bg-zinc-800'
          }`}
        >
          <Bookmark className={`w-4 h-4 ${bookmarked ? 'fill-current' : ''}`} />
        </button>
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
