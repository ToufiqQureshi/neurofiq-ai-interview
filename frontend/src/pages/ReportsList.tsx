import { useEffect, useState } from 'react';
import { Search } from 'lucide-react';
import { Link, useNavigate } from 'react-router-dom';

export function ReportsList() {
  const [reports, setReports] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState('');
  const navigate = useNavigate();

  useEffect(() => {
    fetch(`${import.meta.env.VITE_API_URL}/api/reports`, { credentials: 'include' })
      .then(r => r.ok ? r.json() : (r.status === 401 && navigate('/auth'), null))
      .then(d => setReports(Array.isArray(d) ? d : []))
      .catch(console.error)
      .finally(() => setLoading(false));
  }, [navigate]);

  const filteredReports = reports.filter(report =>
    (report.repo_full_name || '').toLowerCase().includes(query.trim().toLowerCase())
  );

  return (
    <div className="p-6 md:p-8 max-w-7xl mx-auto space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="font-display text-2xl font-extrabold text-ink">All Reports</h1>
          <p className="text-sm text-ink-faint mt-1">Review your past interview evaluations.</p>
        </div>

        <div className="relative w-full sm:w-72">
          <Search className="absolute left-4 top-1/2 -translate-y-1/2 text-ink-faint w-4 h-4" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search reports..."
            className="w-full bg-surface border border-line rounded-full py-2 pl-10 pr-4 text-sm text-ink font-mono focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent transition-all"
          />
        </div>
      </div>

      <div className="bg-surface border border-line rounded-xl overflow-hidden">
        <div className="divide-y divide-line">
          {loading ? (
             <div className="p-8 text-center text-sm text-ink-faint">Loading reports...</div>
          ) : reports.length === 0 ? (
             <div className="p-8 text-center text-sm text-ink-faint">No reports found.</div>
          ) : filteredReports.length === 0 ? (
             <div className="p-8 text-center text-sm text-ink-faint">No reports match "{query}".</div>
          ) : (
            filteredReports.map(report => (
              <div key={report.id} className="p-4 flex items-center justify-between hover:bg-paper/60 transition-colors group">
                <div className="flex items-center gap-4">
                  <div className="w-10 h-10 rounded-lg bg-paper border border-line flex items-center justify-center text-ink-soft text-xs font-mono font-bold uppercase flex-shrink-0">
                    {(report.repo_full_name || '??').slice(0, 2)}
                  </div>
                  <div>
                    <Link to={`/report/${report.id}`} className="font-semibold text-sm text-ink hover:text-accent transition-colors">
                      {report.repo_full_name || 'Unknown repository'}
                    </Link>
                    <div className="text-xs text-ink-faint mt-0.5 flex items-center gap-1 font-mono">
                      <span>Session {String(report.id).slice(0,8).toUpperCase()}</span>
                      <span>•</span>
                      <span>{new Date(report.created_at).toLocaleDateString()} {new Date(report.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                    </div>
                  </div>
                </div>
                <div className="flex flex-col items-end gap-1">
                  <span className={`lowercase px-3 py-1 rounded-full text-[11px] font-semibold ${
                    report.overall_score >= 8 ? 'bg-pass-soft text-pass' :
                    report.overall_score >= 5 ? 'bg-warn-soft text-warn' :
                    'bg-crit-soft text-crit'
                  }`}>
                    {report.overall_score >= 8 ? 'excellent' : report.overall_score >= 5 ? 'average' : 'needs review'}
                  </span>
                  <span className="text-xs font-mono font-medium text-ink-faint text-right tabular-nums">{Math.round((report.overall_score || 0) * 10)}% score</span>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
