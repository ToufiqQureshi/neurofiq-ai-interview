import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';

export function Dashboard() {
  const [reports, setReports] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();
  const { user } = useAuth();

  useEffect(() => {
    fetch(`${import.meta.env.VITE_API_URL}/api/reports`, { credentials: 'include' })
      .then(r => r.ok ? r.json() : (r.status === 401 && navigate('/auth'), null))
      .then(d => setReports(Array.isArray(d) ? d : []))
      .catch(console.error)
      .finally(() => setLoading(false));
  }, [navigate]);

  // overall_score comes back on a 0-10 scale (see Report.tsx's "x.x/10"); convert to a percentage for display.
  const totalInterviews = reports.length;
  const averageScore = totalInterviews > 0
    ? Math.round(reports.reduce((acc, curr) => acc + (curr.overall_score || 0), 0) / totalInterviews * 10)
    : 0;
  const connectedRepos = new Set(reports.map(r => r.repo_full_name)).size;
  const freeRemaining = Math.max(5 - totalInterviews, 0);

  return (
    <div className="p-6 md:p-8 max-w-7xl mx-auto space-y-5">
      {/* Header */}
      <div>
        <h1 className="font-display text-2xl font-extrabold text-ink">Dashboard</h1>
        <p className="text-sm text-ink-faint mt-1">Welcome back, {user?.github_username || 'there'}.</p>
      </div>

      {/* Metrics Row */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div className="relative bg-surface border border-line rounded-xl p-5">
          <span className="absolute top-4 right-4 w-2 h-2 rounded-full bg-accent"></span>
          <p className="text-[10px] font-mono font-semibold text-ink-faint uppercase tracking-wider mb-2">Total Interviews</p>
          <h2 className="font-display text-3xl font-extrabold text-ink tabular-nums">{totalInterviews}</h2>
        </div>

        <div className="relative bg-surface border border-line rounded-xl p-5">
          <span className="absolute top-4 right-4 w-2 h-2 rounded-full bg-accent"></span>
          <p className="text-[10px] font-mono font-semibold text-ink-faint uppercase tracking-wider mb-2">Average Score</p>
          <h2 className="font-display text-3xl font-extrabold text-ink tabular-nums">{averageScore}%</h2>
        </div>

        <div className="relative bg-surface border border-line rounded-xl p-5">
          <span className="absolute top-4 right-4 w-2 h-2 rounded-full bg-accent"></span>
          <p className="text-[10px] font-mono font-semibold text-ink-faint uppercase tracking-wider mb-2">Repos Interviewed</p>
          <h2 className="font-display text-3xl font-extrabold text-ink tabular-nums">{connectedRepos}</h2>
        </div>
      </div>

      {/* Recent Sessions */}
      <div className="bg-surface border border-line rounded-xl overflow-hidden">
        <div className="flex justify-between items-center p-5 border-b border-line">
          <h3 className="font-display font-bold text-ink text-base">Recent Sessions</h3>
          <Link to="/reports" className="text-xs font-mono font-semibold text-accent hover:underline">view all &rarr;</Link>
        </div>

        <div className="divide-y divide-line">
          {loading ? (
             <div className="p-8 text-center text-sm text-ink-faint">Loading sessions...</div>
          ) : reports.length === 0 ? (
             <div className="p-8 text-center text-sm text-ink-faint">No interviews taken yet.</div>
          ) : (
            reports.slice(0, 5).map(report => (
              <div key={report.id} className="p-4 flex items-center justify-between hover:bg-paper/60 transition-colors">
                <div className="flex items-center gap-4">
                  <div className="w-10 h-10 rounded-lg bg-paper border border-line flex items-center justify-center text-ink-soft text-xs font-mono font-bold uppercase">
                    {(report.repo_full_name || '??').slice(0, 2)}
                  </div>
                  <div>
                    <Link to={`/report/${report.id}`} className="font-semibold text-sm text-ink hover:text-accent transition-colors">
                      {report.repo_full_name || 'Unknown repository'}
                    </Link>
                    <div className="text-xs text-ink-faint mt-0.5 flex items-center gap-1 font-mono">
                      <span>{String(report.id).slice(0,8).toUpperCase()}</span>
                      <span>&middot;</span>
                      <span>{new Date(report.created_at).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' })}</span>
                    </div>
                  </div>
                </div>
                <span className={`lowercase px-3 py-1 rounded-full text-[11px] font-semibold ${
                  report.overall_score >= 8 ? 'bg-pass-soft text-pass' :
                  report.overall_score >= 5 ? 'bg-warn-soft text-warn' :
                  'bg-crit-soft text-crit'
                }`}>
                  {report.overall_score >= 8 ? 'excellent' : report.overall_score >= 5 ? 'average' : 'needs review'}
                </span>
              </div>
            ))
          )}
        </div>
      </div>

      {/* Action bar */}
      <div className="bg-ink rounded-xl px-6 py-5 flex items-center justify-between gap-4 flex-wrap">
        <div>
          <p className="text-[10px] font-mono font-semibold text-white/40 uppercase tracking-wider mb-1">Action Needed</p>
          <p className="text-white font-semibold">{freeRemaining} free interview{freeRemaining === 1 ? '' : 's'} remaining</p>
        </div>
        <Link to="/repositories" className="bg-accent hover:bg-accent-dark text-white font-semibold text-sm px-5 py-2.5 rounded-full transition-colors">
          Start New Interview
        </Link>
      </div>
    </div>
  );
}
