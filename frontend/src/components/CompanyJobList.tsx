import { useEffect, useState } from 'react';
import { Sparkles, ExternalLink } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

interface Job {
  id: string;
  company_id: string;
  company_name: string;
  title: string;
  department: string;
  location: string;
  url: string;
  posted_at: string;
  field: string;
  level: string;
}

interface CompanyJobListProps {
  companyId: string;
  companyName: string;
  field?: string;
  level?: string;
  onJobSelect?: (job: Job) => void;
}

export default function CompanyJobList({
  companyId,
  companyName,
  field,
  level,
}: CompanyJobListProps) {
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

  if (loading) {
    return (
      <div className="py-3 px-2 text-center">
        <p className="text-xs text-ink-faint font-mono animate-pulse">Loading live roles…</p>
      </div>
    );
  }

  if (!jobs || jobs.length === 0) {
    return (
      <div className="py-2 px-1 text-center">
        <p className="text-xs text-ink-faint">No active postings right now.</p>
      </div>
    );
  }

  return (
    <ul className="divide-y divide-line max-h-56 overflow-y-auto -mx-1 pr-1 space-y-1">
      {jobs.map(j => (
        <li key={j.id} className="py-2 px-1.5 rounded-lg hover:bg-slate-50 transition-colors">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0 flex-1">
              <p className="text-xs font-semibold text-ink truncate leading-tight" title={j.title}>{j.title}</p>
              <p className="text-[10px] text-ink-faint truncate mt-0.5 font-mono">
                {[j.department, j.location].filter(Boolean).join(' · ')}
              </p>
            </div>
            <div className="flex items-center gap-1.5 flex-shrink-0">
              <button
                onClick={e => {
                  e.stopPropagation();
                  // Carry the role through repo selection and analysis so the
                  // interview can be framed by it. It is a query param rather
                  // than component state because the two screens in between
                  // are full navigations.
                  navigate(`/repositories?job=${encodeURIComponent(j.id)}`);
                }}
                title={`Practice AI Interview for ${j.title} at ${companyName}`}
                className="px-2 py-0.5 rounded-md text-[10px] font-semibold bg-accent-soft text-accent hover:bg-accent hover:text-white transition-all flex items-center gap-1 shadow-sm"
              >
                <Sparkles className="w-2.5 h-2.5" /> Mock
              </button>
              {j.url && (
                <a
                  href={j.url}
                  target="_blank"
                  rel="noreferrer"
                  title="Open live posting"
                  className="p-1 rounded text-ink-faint hover:text-ink hover:bg-slate-200 transition-colors"
                >
                  <ExternalLink className="w-3 h-3" />
                </a>
              )}
            </div>
          </div>
        </li>
      ))}
    </ul>
  );
}
