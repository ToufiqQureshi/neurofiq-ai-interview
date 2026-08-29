import { useEffect, useState } from 'react';
import { Search, GitBranch } from 'lucide-react';
import { Link, useNavigate } from 'react-router-dom';

export function Repositories() {
  const [repos, setRepos] = useState<any[]>([]);
  const [analysisStatus, setAnalysisStatus] = useState<Record<string, string>>({});
  const [used, setUsed] = useState(0);
  const [limit, setLimit] = useState(3);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [query, setQuery] = useState('');
  const navigate = useNavigate();

  useEffect(() => {
    fetch(`${import.meta.env.VITE_API_URL}/api/repos`, { credentials: 'include' })
      .then(r => {
        if (r.status === 401) {
          navigate('/auth');
          return null;
        }
        if (!r.ok) throw new Error('Could not load repositories');
        return r.json();
      })
      .then(d => {
        if (!d) return;
        setRepos(d.repos || []);
        setAnalysisStatus(d.analysis_status || {});
        setUsed(d.analyses_used || 0);
        setLimit(d.analyses_limit || 3);
      })
      .catch(err => setError(err.message))
      .finally(() => setLoading(false));
  }, [navigate]);

  const filteredRepos = repos.filter(repo =>
    repo.name?.toLowerCase().includes(query.trim().toLowerCase())
  );

  return (
    <div className="p-6 md:p-8 max-w-7xl mx-auto space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="font-display text-2xl font-extrabold text-ink">Repositories</h1>
          <p className="text-sm text-ink-faint mt-1">Pick any of your repos. Free tier: {used}/{limit} analyses used. You choose which three.</p>
        </div>

        <div className="relative w-full sm:w-72">
          <Search className="absolute left-4 top-1/2 -translate-y-1/2 text-ink-faint w-4 h-4" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search repositories..."
            className="w-full bg-surface border border-line rounded-full py-2 pl-10 pr-4 text-sm text-ink font-mono focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent transition-all"
          />
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {loading ? (
           <div className="col-span-full text-center py-12 text-sm text-ink-faint bg-surface border border-line rounded-xl">Loading repositories...</div>
        ) : error ? (
           <div className="col-span-full text-center py-12 text-sm text-crit bg-surface border border-line rounded-xl">{error}</div>
        ) : repos.length === 0 ? (
           <div className="col-span-full text-center py-12 text-sm text-ink-faint bg-surface border border-line rounded-xl">No repositories found. Ensure you have granted GitHub access.</div>
        ) : filteredRepos.length === 0 ? (
           <div className="col-span-full text-center py-12 text-sm text-ink-faint bg-surface border border-line rounded-xl">No repositories match "{query}".</div>
        ) : filteredRepos.map((repo) => (
          <div key={repo.id} className="bg-surface border border-line rounded-xl p-5 flex flex-col hover:border-line-strong transition-all group">
            <div className="flex justify-between items-start mb-3">
               <div className="flex items-center gap-3">
                 <div className="w-10 h-10 rounded-lg bg-paper border border-line flex items-center justify-center flex-shrink-0">
                   <GitBranch className="text-ink-soft w-5 h-5" />
                 </div>
                 <div>
                   <h3 className="font-semibold text-sm text-ink truncate" title={repo.name}>{repo.name}</h3>
                   <div className="flex items-center gap-2 mt-0.5">
                     <span className="flex items-center gap-1.5 text-xs text-ink-faint font-mono">
                       <span className="w-2 h-2 rounded-full bg-accent"></span>
                       {repo.language || "Code"}
                     </span>
                   </div>
                 </div>
               </div>
            </div>

            <p className="text-ink-soft text-xs mb-6 line-clamp-2 h-8 flex-1 leading-relaxed">
              {repo.description || "No description provided for this repository."}
            </p>

            {(() => {
              const status = analysisStatus[repo.full_name];
              const label = status === 'pending' ? 'Analysis running…' : status === 'failed' ? 'Retry analysis' : status && status !== 'failed' ? 'Continue interview' : 'Analyze & interview';
              return (
                <Link to={`/analyze/${encodeURIComponent(repo.full_name)}`} className="block w-full py-2.5 text-center text-xs font-semibold text-white bg-ink hover:bg-black rounded-full transition-colors">
                  {label}
                </Link>
              );
            })()}
          </div>
        ))}
      </div>
    </div>
  );
}
