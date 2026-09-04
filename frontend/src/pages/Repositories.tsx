import { useEffect, useMemo, useState } from 'react';
import { Search, GitBranch, Check, Loader2, RotateCcw, ArrowRight } from 'lucide-react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { readTarget, targetQuery } from '../lib/interviewTarget';
import { useAuth } from '../context/AuthContext';

type Repo = {
  id: number;
  name: string;
  full_name: string;
  description: string;
  language: string;
  private: boolean;
};

const api = import.meta.env.VITE_API_URL;

// Repositories is where a candidate commits to the repos they'll be
// interviewed on.
//
// Deliberately a selectable grid rather than a dropdown: a native select has
// no search, shows nothing but the name, and hands mobile users an OS picker.
// Choosing three repositories out of sixty is a comparison task — language,
// description and recency are what make the choice, so they have to be on
// screen while you choose.
export function Repositories() {
  const [repos, setRepos] = useState<Repo[]>([]);
  const [analysisStatus, setAnalysisStatus] = useState<Record<string, string>>({});
  const [used, setUsed] = useState(0);
  const [limit, setLimit] = useState(3);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<string[]>([]);
  const [starting, setStarting] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const { user } = useAuth();
  const [loadFailed, setLoadFailed] = useState(false);

  useEffect(() => {
    fetch(`${api}/api/repos`, { credentials: 'include' })
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
      .catch(() => setLoadFailed(true))
      .finally(() => setLoading(false));
  }, [navigate]);

  const remaining = Math.max(limit - used, 0);

  const filteredRepos = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return repos;
    return repos.filter(
      repo =>
        repo.name?.toLowerCase().includes(q) ||
        repo.description?.toLowerCase().includes(q) ||
        repo.language?.toLowerCase().includes(q),
    );
  }, [repos, query]);

  // The limit check reads `selected` directly rather than running inside a
  // setSelected updater. Updaters have to be pure — React calls them twice in
  // StrictMode — so raising the error in there fired the side effect twice.
  const toggle = (fullName: string) => {
    setError('');

    if (selected.includes(fullName)) {
      setSelected(selected.filter(n => n !== fullName));
      return;
    }
    if (selected.length >= remaining) {
      setError(
        remaining === 0
          ? `You've used all ${limit} analysis slots.`
          : `You can pick ${remaining} more repositor${remaining === 1 ? 'y' : 'ies'} on the free tier.`,
      );
      return;
    }
    setSelected([...selected, fullName]);
  };

  // Start every selected analysis, then follow the first one. The rest keep
  // running in the background and show as "Analysis running" when the user
  // comes back to this page.
  const startAnalyses = async () => {
    if (selected.length === 0) return;
    setStarting(true);
    setError('');
    try {
      for (const fullName of selected) {
        const res = await fetch(`${api}/api/repos/analyze`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'include',
          body: JSON.stringify({ repo_full_name: fullName }),
        });
        if (!res.ok) {
          const data = await res.json().catch(() => null);
          throw new Error(data?.error || `Could not start the analysis for ${fullName}.`);
        }
      }
      // Carries the Job Map role the candidate started from, if any.
      navigate(`/analyze/${encodeURIComponent(selected[0])}${targetQuery(readTarget(location.search))}`);
    } catch (err: any) {
      setError(err.message);
      setStarting(false);
    }
  };

  const statusLabel = (status: string) => {
    if (status === 'pending') return { text: 'Analysis running', tone: 'text-warn bg-warn-soft' };
    if (status === 'failed') return { text: 'Failed — retry', tone: 'text-crit bg-crit-soft' };
    return { text: 'Analyzed', tone: 'text-pass bg-pass-soft' };
  };

  return (
    <div className={`p-6 md:p-8 max-w-7xl mx-auto space-y-6 ${selected.length > 0 ? 'pb-28' : ''}`}>
      <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-4">
        <div>
          <h1 className="font-display text-2xl font-extrabold text-ink">Pick your repositories</h1>
          <p className="text-sm text-ink-faint mt-1 max-w-prose">
            Choose the code you want to be interviewed on. We read the repository and its commit
            history, then ask about the decisions you made in it.
          </p>
          <div className="flex items-center gap-2 mt-3">
            {/* A pill per slot only reads at single digits — the free tier
                is 100 slots, which drew a 2,800px row with no wrap and
                pushed the page into horizontal scroll at every width. One
                proportional bar scales to any limit. */}
            <div
              className="w-32 h-1.5 rounded-full bg-line overflow-hidden flex"
              aria-hidden="true"
            >
              <div
                className="h-full bg-ink"
                style={{ width: `${limit > 0 ? Math.min(100, (used / limit) * 100) : 0}%` }}
              />
              <div
                className="h-full bg-accent"
                style={{ width: `${limit > 0 ? Math.min(100, (selected.length / limit) * 100) : 0}%` }}
              />
            </div>
            <span className="text-xs font-mono text-ink-faint tabular-nums">
              {used} used · {remaining} left on the free tier
            </span>
          </div>
        </div>

        <div className="relative w-full sm:w-72 shrink-0">
          <Search className="absolute left-4 top-1/2 -translate-y-1/2 text-ink-faint w-4 h-4" />
          <input
            type="text"
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="Search name, language, description…"
            className="w-full bg-surface border border-line rounded-full py-2 pl-10 pr-4 text-sm text-ink font-mono focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent transition-all"
          />
        </div>
      </div>

      {error && (
        <div role="alert" className="bg-crit-soft border border-crit/20 text-crit rounded-xl px-4 py-3 text-sm">
          {error}
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {loading ? (
          <div className="col-span-full text-center py-12 text-sm text-ink-faint bg-surface border border-line rounded-xl">
            Loading repositories...
          </div>
        ) : loadFailed ? (
          <div className="col-span-full text-center py-12 text-sm text-ink-faint bg-surface border border-line rounded-xl">
            Couldn't reach the server. Check your connection and try again.
          </div>
        ) : !user?.github_connected ? (
          <div className="col-span-full flex flex-col items-center gap-3 text-center py-12 text-sm text-ink-faint bg-surface border border-line rounded-xl">
            <p>Connect your GitHub account to pick a repository to be interviewed on.</p>
            <a
              href={`${api}/auth/github/login`}
              className="px-4 py-2 rounded-full bg-ink hover:bg-black text-white text-xs font-semibold transition-colors"
            >
              Connect GitHub
            </a>
          </div>
        ) : repos.length === 0 ? (
          <div className="col-span-full text-center py-12 text-sm text-ink-faint bg-surface border border-line rounded-xl">
            No repositories found on this GitHub account.
          </div>
        ) : filteredRepos.length === 0 ? (
          <div className="col-span-full text-center py-12 text-sm text-ink-faint bg-surface border border-line rounded-xl">
            No repositories match "{query}".
          </div>
        ) : (
          filteredRepos.map(repo => {
            const status = analysisStatus[repo.full_name];
            const isSelected = selected.includes(repo.full_name);

            // A repo that already holds a slot isn't part of the picking —
            // it links straight through to its interview or its retry.
            if (status && status !== 'failed') {
              const badge = statusLabel(status);
              return (
                <div
                  key={repo.id}
                  className="bg-surface border border-line rounded-xl p-5 flex flex-col"
                >
                  <RepoHeader repo={repo} />
                  <RepoDescription repo={repo} />
                  <div className="flex items-center gap-2">
                    <span className={`text-[10px] font-mono uppercase tracking-wider px-2 py-1 rounded-full ${badge.tone}`}>
                      {badge.text}
                    </span>
                  </div>
                  <Link
                    to={`/analyze/${encodeURIComponent(repo.full_name)}${targetQuery(readTarget(location.search))}`}
                    className="mt-4 block w-full py-2.5 text-center text-xs font-semibold text-white bg-ink hover:bg-black rounded-full transition-colors"
                  >
                    {status === 'pending' ? 'View progress' : 'Continue interview'}
                  </Link>
                </div>
              );
            }

            return (
              <button
                key={repo.id}
                type="button"
                onClick={() => toggle(repo.full_name)}
                aria-pressed={isSelected}
                className={`text-left bg-surface border rounded-xl p-5 flex flex-col transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-accent ${
                  isSelected
                    ? 'border-accent ring-1 ring-accent'
                    : 'border-line hover:border-line-strong'
                }`}
              >
                <div className="flex justify-between items-start gap-3 mb-3">
                  <RepoHeader repo={repo} />
                  <span
                    className={`w-5 h-5 rounded-md border flex items-center justify-center flex-shrink-0 transition-colors ${
                      isSelected ? 'bg-accent border-accent' : 'bg-paper border-line-strong'
                    }`}
                  >
                    {isSelected && <Check className="w-3.5 h-3.5 text-white" strokeWidth={3} />}
                  </span>
                </div>

                <RepoDescription repo={repo} />

                {status === 'failed' ? (
                  <span className="text-[10px] font-mono uppercase tracking-wider px-2 py-1 rounded-full text-crit bg-crit-soft inline-flex items-center gap-1 self-start">
                    <RotateCcw className="w-3 h-3" />
                    Failed — pick again to retry
                  </span>
                ) : (
                  <span className="text-[11px] text-ink-faint font-mono">
                    {isSelected ? 'Selected' : 'Tap to select'}
                  </span>
                )}
              </button>
            );
          })
        )}
      </div>

      {/* Sticky confirm bar. Offset by the sidebar width on desktop so it
          lines up with the content rather than the viewport. */}
      {selected.length > 0 && (
        <div className="fixed bottom-0 left-0 right-0 lg:left-64 z-30 bg-ink border-t border-black/20">
          <div className="max-w-7xl mx-auto px-6 md:px-8 py-4 flex items-center justify-between gap-4">
            <div className="min-w-0">
              <p className="text-white font-semibold text-sm tabular-nums">
                {selected.length} of {remaining} selected
              </p>
              <p className="text-white/50 text-xs truncate font-mono">
                {selected.map(name => name.split('/')[1] || name).join(' · ')}
              </p>
            </div>
            <div className="flex items-center gap-2 flex-shrink-0">
              <button
                onClick={() => setSelected([])}
                disabled={starting}
                className="text-xs font-semibold text-white/60 hover:text-white px-3 py-2 transition-colors disabled:opacity-50"
              >
                Clear
              </button>
              <button
                onClick={startAnalyses}
                disabled={starting}
                className="bg-accent hover:bg-accent-dark text-white font-semibold text-sm px-5 py-2.5 rounded-full transition-colors flex items-center gap-2 disabled:opacity-60"
              >
                {starting ? (
                  <>
                    <Loader2 className="w-4 h-4 animate-spin" />
                    Starting…
                  </>
                ) : (
                  <>
                    Analyze {selected.length === 1 ? 'this repo' : `these ${selected.length}`}
                    <ArrowRight className="w-4 h-4" />
                  </>
                )}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function RepoHeader({ repo }: { repo: Repo }) {
  return (
    <div className="flex items-center gap-3 min-w-0">
      <div className="w-10 h-10 rounded-lg bg-paper border border-line flex items-center justify-center flex-shrink-0">
        <GitBranch className="text-ink-soft w-5 h-5" />
      </div>
      <div className="min-w-0">
        <h3 className="font-semibold text-sm text-ink truncate" title={repo.name}>
          {repo.name}
        </h3>
        <div className="flex items-center gap-2 mt-0.5">
          <span className="flex items-center gap-1.5 text-xs text-ink-faint font-mono">
            <span className="w-2 h-2 rounded-full bg-accent"></span>
            {repo.language || 'Code'}
          </span>
          {repo.private && (
            <span className="text-[10px] font-mono uppercase tracking-wider text-ink-faint">
              Private
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

function RepoDescription({ repo }: { repo: Repo }) {
  return (
    <p className="text-ink-soft text-xs mb-4 line-clamp-2 h-8 flex-1 leading-relaxed">
      {repo.description || 'No description provided for this repository.'}
    </p>
  );
}
