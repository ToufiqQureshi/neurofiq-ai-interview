import { Sparkles, ArrowRight, ShieldCheck, Target } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';

interface AiMatchSummaryCardProps {
  totalJobsCount?: number;
  filteredCount?: number;
}

export function AiMatchSummaryCard({ totalJobsCount = 3318, filteredCount }: AiMatchSummaryCardProps) {
  const navigate = useNavigate();
  const { user } = useAuth();

  return (
    <div className="bg-paper dark:bg-zinc-900/90 border border-line rounded-2xl p-5 shadow-sm sticky top-24 flex flex-col gap-4">
      {/* Header */}
      <div className="flex items-center gap-2.5">
        <div className="w-8 h-8 rounded-xl bg-accent-soft flex items-center justify-center text-accent flex-shrink-0">
          <Sparkles className="w-4 h-4" />
        </div>
        <div>
          <h4 className="text-sm font-bold text-ink">AI Career Copilot</h4>
          <p className="text-[11px] text-ink-faint">Tailored Mock Interviews</p>
        </div>
      </div>

      {/* Candidate Profile / Match Status */}
      {user ? (
        <div className="p-3.5 rounded-xl bg-zinc-50 dark:bg-zinc-800/60 border border-line/60">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold text-ink truncate">
              {user.full_name || user.github_username || 'Engineer'}
            </span>
            <span className="text-[10px] font-mono px-2 py-0.5 rounded-full bg-emerald-50 dark:bg-emerald-950/40 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20">
              {user.experience_level || 'Active Profile'}
            </span>
          </div>
          {user.target_role && (
            <p className="text-[11px] text-accent font-medium mt-1 truncate">
              🎯 Target: {user.target_role}
            </p>
          )}
        </div>
      ) : (
        <div className="p-3.5 rounded-xl bg-zinc-50 dark:bg-zinc-800/60 border border-line/60 text-xs text-ink-soft">
          Sign in and link your GitHub repo to see tailored technical interview questions matched to your exact stack.
        </div>
      )}

      {/* Stats Summary */}
      <div className="grid grid-cols-2 gap-2 pt-1">
        <div className="p-2.5 rounded-xl bg-paper dark:bg-zinc-800/40 border border-line/60 text-center">
          <div className="text-base font-bold text-ink font-mono">
            {filteredCount !== undefined ? filteredCount : totalJobsCount}
          </div>
          <div className="text-[10px] text-ink-faint uppercase font-medium mt-0.5">
            Active Openings
          </div>
        </div>
        <div className="p-2.5 rounded-xl bg-paper dark:bg-zinc-800/40 border border-line/60 text-center">
          <div className="text-base font-bold text-emerald-600 dark:text-emerald-400 font-mono">
            &lt; 24h
          </div>
          <div className="text-[10px] text-ink-faint uppercase font-medium mt-0.5">
            Verified Fresh
          </div>
        </div>
      </div>

      {/* Quick Launch CTA */}
      <button
        type="button"
        onClick={() => navigate('/radar')}
        className="w-full py-2.5 px-4 rounded-xl bg-accent hover:bg-accent/90 text-white text-xs font-semibold shadow-md shadow-accent/20 transition-all flex items-center justify-center gap-2 group"
      >
        <Target className="w-4 h-4" />
        <span>Open Profile Radar</span>
        <ArrowRight className="w-3.5 h-3.5 group-hover:translate-x-0.5 transition-transform" />
      </button>

      <div className="flex items-center gap-1.5 text-[11px] text-ink-faint justify-center">
        <ShieldCheck className="w-3.5 h-3.5 text-emerald-500" />
        <span>100% direct ATS feeds · Zero ghost jobs</span>
      </div>
    </div>
  );
}
