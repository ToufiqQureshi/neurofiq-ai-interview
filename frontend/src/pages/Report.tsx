import { useState, useEffect } from 'react';
import { Target, AlertCircle, RefreshCw, Loader2 } from 'lucide-react';
import { Link, useParams } from 'react-router-dom';

export function Report() {
  const { sessionId } = useParams();
  const [report, setReport] = useState<any>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!sessionId) return;
    fetch(`${import.meta.env.VITE_API_URL}/api/reports/${sessionId}`, {
      credentials: 'include'
    })
    .then(res => {
      if (!res.ok) throw new Error('Report not found');
      return res.json();
    })
    .then(data => {
      // Parse feedback JSON if it's a string
      if (typeof data.feedback_json === 'string') {
        try {
          data.feedback = JSON.parse(data.feedback_json);
        } catch (e) {
          console.error("Failed to parse feedback JSON", e);
        }
      } else {
        data.feedback = data.feedback_json;
      }
      setReport(data);
    })
    .catch(err => setError(err.message));
  }, [sessionId]);

  if (error) {
    return <div className="min-h-screen flex items-center justify-center text-crit">Error: {error}</div>;
  }

  if (!report) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-paper">
        <Loader2 className="w-10 h-10 text-accent animate-spin" />
      </div>
    );
  }

  const overallFeedback = report.feedback?.overall_feedback || "No overall feedback available.";
  const detailedFeedback = report.feedback?.detailed_feedback || [];
  const overallScore = report.overall_score ?? 0;

  return (
    <div className="min-h-screen bg-paper text-ink p-8">
      <div className="max-w-4xl mx-auto space-y-6">

        {/* Header */}
        <div className="flex items-center justify-between border-b border-line pb-6">
          <div>
            <div className="text-[11px] font-mono uppercase tracking-widest text-ink-faint mb-1">Interview Report</div>
            <h1 className="font-display text-3xl font-extrabold">{report.repo_full_name || 'Unknown repository'}</h1>
          </div>
          <div className="text-center">
            <div className="font-mono text-5xl font-medium text-pass tabular-nums">{overallScore.toFixed(1)}<span className="text-2xl text-ink-faint">/10</span></div>
            <div className="text-[10px] font-mono text-ink-faint font-medium uppercase tracking-wider mt-1">Overall Score</div>
          </div>
        </div>

        {/* Overall Feedback */}
        <div className="bg-surface border border-line rounded-xl p-6">
          <h3 className="font-display text-xl font-semibold mb-3">Overall Impression</h3>
          <p className="text-ink-soft leading-relaxed">{overallFeedback}</p>
        </div>

        {/* Detailed Feedback Sections */}
        {detailedFeedback.map((item: any, idx: number) => {
          const score = Number(item.score) || 0;
          const band = score >= 8
            ? { stripe: 'bg-pass', chip: 'bg-pass-soft', text: 'text-pass' }
            : score >= 5
            ? { stripe: 'bg-warn', chip: 'bg-warn-soft', text: 'text-warn' }
            : { stripe: 'bg-crit', chip: 'bg-crit-soft', text: 'text-crit' };
          return (
            <div key={idx} className="relative bg-surface border border-line rounded-xl overflow-hidden">
              <div className={`absolute left-0 top-0 bottom-0 w-1 ${band.stripe}`}></div>
              <div className="p-6 pl-7 space-y-4">
                <div className="flex justify-between items-start gap-4">
                  <div>
                    <div className="text-[10px] font-mono uppercase tracking-widest text-ink-faint mb-1">Question {idx + 1}</div>
                    <h3 className="text-lg font-semibold flex-1">{item.question}</h3>
                  </div>
                  <div className={`w-12 h-12 rounded-lg ${band.chip} flex items-center justify-center flex-shrink-0 text-lg font-mono font-bold ${band.text}`}>
                    {item.score}
                  </div>
                </div>

                <div className={`grid grid-cols-1 ${item.strengths ? 'md:grid-cols-2' : ''} gap-6 pt-4 border-t border-line`}>
                  {item.strengths && (
                    <section>
                      <h4 className="text-sm font-semibold mb-2 text-pass flex items-center gap-2">
                        <Target className="w-4 h-4" /> Strengths
                      </h4>
                      <p className="text-ink-soft text-sm">{item.strengths}</p>
                    </section>
                  )}

                  <section>
                    <h4 className="text-sm font-semibold mb-2 text-warn flex items-center gap-2">
                      <AlertCircle className="w-4 h-4" /> Areas for Improvement
                    </h4>
                    <p className="text-ink-soft text-sm">{item.areas_for_improvement}</p>
                  </section>
                </div>

                <div className="bg-paper p-4 rounded-lg mt-4 text-sm text-ink-soft border border-line">
                  <span className="font-semibold text-ink">Ideal Answer Concept: </span>
                  {item.ideal_answer_concept}
                </div>
              </div>
            </div>
          );
        })}

        {/* Actions */}
        <div className="flex gap-3 justify-end pt-4">
          <Link to="/dashboard" className="px-6 py-3 bg-surface border border-line-strong hover:bg-paper text-ink rounded-full font-semibold transition-colors">
            Back to Dashboard
          </Link>
          <button className="px-6 py-3 bg-ink text-white rounded-full font-semibold transition-colors flex items-center gap-2 disabled:opacity-50" disabled>
            <RefreshCw className="w-5 h-5" />
            Retry Weak Areas (Coming Soon)
          </button>
        </div>

      </div>
    </div>
  );
}
