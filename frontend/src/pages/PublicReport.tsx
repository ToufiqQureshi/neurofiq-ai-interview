import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { Loader2, ShieldCheck } from 'lucide-react';
import { GithubIcon } from '../components/GithubIcon';

type Feedback = {
  question: string;
  score: number;
  strengths: string;
  areas_for_improvement: string;
};

type PublicReportData = {
  repo_full_name: string;
  overall_score: number;
  overall_feedback: string;
  detailed_feedback: Feedback[] | null;
  mode: string;
  created_at: string;
  candidate: { github_username: string; avatar_url: string };
};

// PublicReport is what a candidate actually sends to a hiring manager.
//
// It reads without an account, shows the score and the assessment, and says
// plainly what the score is evidence of — that the questions came from this
// person's own repository. The answers themselves stay private.
export function PublicReport() {
  const { slug } = useParams();
  const [report, setReport] = useState<PublicReportData | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!slug) return;
    fetch(`${import.meta.env.VITE_API_URL}/api/public/reports/${encodeURIComponent(slug)}`)
      .then(async res => {
        if (!res.ok) throw new Error('This report is not available. The link may have been turned off.');
        return res.json();
      })
      .then(setReport)
      .catch(err => setError(err.message));
  }, [slug]);

  if (error) {
    return (
      <div className="min-h-screen bg-paper flex flex-col items-center justify-center gap-4 p-8 text-center">
        <h1 className="font-display text-2xl font-bold text-ink">Report unavailable</h1>
        <p className="text-ink-soft max-w-md">{error}</p>
        <Link to="/" className="text-accent font-semibold hover:underline">Go to NeuroFIQ</Link>
      </div>
    );
  }

  if (!report) {
    return (
      <div className="min-h-screen bg-paper flex items-center justify-center">
        <Loader2 className="w-10 h-10 text-accent animate-spin" />
      </div>
    );
  }

  const score = report.overall_score ?? 0;
  const feedback = report.detailed_feedback || [];
  const taken = new Date(report.created_at).toLocaleDateString(undefined, {
    year: 'numeric', month: 'long', day: 'numeric',
  });

  return (
    <div className="min-h-screen bg-paper text-ink">
      <header className="border-b border-line bg-surface">
        <div className="max-w-4xl mx-auto px-6 h-16 flex items-center justify-between">
          <Link to="/" className="flex items-center gap-2">
            <span className="w-7 h-7 rounded-md bg-ink text-white flex items-center justify-center text-sm font-display font-extrabold">N</span>
            <span className="font-display font-extrabold text-lg tracking-tight">NeuroFIQ</span>
          </Link>
          <Link
            to="/auth"
            className="text-sm font-semibold px-4 py-2 rounded-full bg-ink text-white hover:bg-black transition-colors"
          >
            Interview your own repo
          </Link>
        </div>
      </header>

      <main className="max-w-4xl mx-auto px-6 py-10 space-y-6">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-6 border-b border-line pb-8">
          <div className="flex items-center gap-4">
            {report.candidate?.avatar_url ? (
              <img
                src={report.candidate.avatar_url}
                alt={report.candidate.github_username}
                className="w-14 h-14 rounded-full object-cover"
              />
            ) : (
              <div className="w-14 h-14 rounded-full bg-accent text-white flex items-center justify-center font-mono font-bold">
                {(report.candidate?.github_username || '??').slice(0, 2).toUpperCase()}
              </div>
            )}
            <div>
              <div className="text-[11px] font-mono uppercase tracking-widest text-ink-faint mb-1">
                Technical interview
              </div>
              <h1 className="font-display text-2xl font-extrabold">
                {report.candidate?.github_username || 'Candidate'}
              </h1>
              <p className="text-sm text-ink-faint font-mono flex items-center gap-1.5 mt-1">
                <GithubIcon className="w-3.5 h-3.5" />
                {report.repo_full_name}
              </p>
            </div>
          </div>

          <div className="text-center sm:text-right">
            <div className="font-mono text-5xl font-medium text-pass tabular-nums">
              {score.toFixed(1)}<span className="text-2xl text-ink-faint">/10</span>
            </div>
            <div className="text-[10px] font-mono text-ink-faint uppercase tracking-wider mt-1">
              {report.mode === 'voice' ? 'Voice interview' : 'Text interview'} · {taken}
            </div>
          </div>
        </div>

        <div className="flex items-start gap-3 bg-accent-soft border border-accent/20 rounded-xl p-4">
          <ShieldCheck className="w-5 h-5 text-accent flex-shrink-0 mt-0.5" />
          <p className="text-sm text-ink-soft">
            Every question below was generated from the code and commit history in{' '}
            <span className="font-mono text-ink">{report.repo_full_name}</span>, then scored on
            correctness, depth, clarity and trade-off awareness.
          </p>
        </div>

        {report.overall_feedback && (
          <div className="bg-surface border border-line rounded-xl p-6">
            <h2 className="font-display text-xl font-semibold mb-3">Overall impression</h2>
            <p className="text-ink-soft leading-relaxed">{report.overall_feedback}</p>
          </div>
        )}

        {feedback.map((item, idx) => {
          const s = Number(item.score) || 0;
          const band = s >= 8
            ? { chip: 'bg-pass-soft', text: 'text-pass' }
            : s >= 5
            ? { chip: 'bg-warn-soft', text: 'text-warn' }
            : { chip: 'bg-crit-soft', text: 'text-crit' };
          return (
            <div key={idx} className="bg-surface border border-line rounded-xl p-6 space-y-4">
              <div className="flex justify-between items-start gap-4">
                <div>
                  <div className="text-[10px] font-mono uppercase tracking-widest text-ink-faint mb-1">
                    Question {idx + 1}
                  </div>
                  <h3 className="text-lg font-semibold">{item.question}</h3>
                </div>
                <div className={`w-12 h-12 rounded-lg ${band.chip} ${band.text} flex items-center justify-center flex-shrink-0 text-lg font-mono font-bold tabular-nums`}>
                  {s}
                </div>
              </div>
              {item.strengths && (
                <p className="text-sm text-ink-soft pt-4 border-t border-line">{item.strengths}</p>
              )}
            </div>
          );
        })}

        <div className="bg-ink rounded-xl px-6 py-8 text-center space-y-3">
          <p className="text-white font-display text-xl font-bold">
            Get interviewed on your own code.
          </p>
          <p className="text-white/60 text-sm max-w-md mx-auto">
            NeuroFIQ reads your repository and asks the questions a Principal Engineer would.
          </p>
          <Link
            to="/auth"
            className="inline-block bg-accent hover:bg-accent-dark text-white font-semibold text-sm px-6 py-3 rounded-full transition-colors"
          >
            Start free
          </Link>
        </div>
      </main>
    </div>
  );
}
