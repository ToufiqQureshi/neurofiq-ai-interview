import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { Loader2, ArrowLeft } from 'lucide-react';

// RecruiterReport shows one candidate's full interview — including their
// answers, which the public share link deliberately withholds. The API only
// serves this for interviews taken under one of the recruiter's own invites.
export function RecruiterReport() {
  const { sessionId } = useParams();
  const [report, setReport] = useState<any>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!sessionId) return;
    fetch(`${import.meta.env.VITE_API_URL}/api/recruiter/reports/${sessionId}`, { credentials: 'include' })
      .then(async res => {
        if (!res.ok) throw new Error('Report not found, or it was not taken through one of your invites.');
        return res.json();
      })
      .then(data => {
        try {
          data.feedback = typeof data.feedback_json === 'string'
            ? JSON.parse(data.feedback_json)
            : data.feedback_json;
        } catch {
          data.feedback = null;
        }
        try {
          data.qa = typeof data.answers_json === 'string' ? JSON.parse(data.answers_json) : data.answers_json;
        } catch {
          data.qa = [];
        }
        setReport(data);
      })
      .catch(err => setError(err.message));
  }, [sessionId]);

  if (error) {
    return (
      <div className="p-8 space-y-4">
        <p className="text-crit">{error}</p>
        <Link to="/hiring" className="text-accent font-semibold hover:underline">Back to candidates</Link>
      </div>
    );
  }

  if (!report) {
    return (
      <div className="p-8 flex justify-center">
        <Loader2 className="w-8 h-8 text-accent animate-spin" />
      </div>
    );
  }

  const detailed = report.feedback?.detailed_feedback || [];
  const answers: { question: string; answer: string }[] = report.qa || [];
  const score = report.overall_score ?? 0;

  return (
    <div className="p-6 md:p-8 max-w-4xl mx-auto space-y-6">
      <Link to="/hiring" className="inline-flex items-center gap-2 text-sm text-ink-soft hover:text-ink">
        <ArrowLeft className="w-4 h-4" />
        Back to candidates
      </Link>

      <div className="flex items-center justify-between border-b border-line pb-6 gap-4 flex-wrap">
        <div>
          <div className="text-[11px] font-mono uppercase tracking-widest text-ink-faint mb-1">Candidate report</div>
          <h1 className="font-display text-2xl font-extrabold">{report.repo_full_name}</h1>
        </div>
        <div className="text-center">
          <div className="font-mono text-4xl font-medium text-pass tabular-nums">
            {score.toFixed(1)}<span className="text-xl text-ink-faint">/10</span>
          </div>
        </div>
      </div>

      {report.feedback?.overall_feedback && (
        <div className="bg-surface border border-line rounded-xl p-6">
          <h2 className="font-display text-lg font-semibold mb-3">Overall impression</h2>
          <p className="text-ink-soft leading-relaxed">{report.feedback.overall_feedback}</p>
        </div>
      )}

      {detailed.map((item: any, idx: number) => (
        <div key={idx} className="bg-surface border border-line rounded-xl p-6 space-y-4">
          <div className="flex justify-between items-start gap-4">
            <div>
              <div className="text-[10px] font-mono uppercase tracking-widest text-ink-faint mb-1">
                Question {idx + 1}
              </div>
              <h3 className="text-base font-semibold">{item.question}</h3>
            </div>
            <div className="font-mono text-xl font-bold tabular-nums text-ink flex-shrink-0">{item.score}</div>
          </div>

          {answers[idx]?.answer && (
            <div className="bg-paper border border-line rounded-lg p-4">
              <div className="text-[10px] font-mono uppercase tracking-wider text-ink-faint mb-2">
                Their answer
              </div>
              <p className="text-sm text-ink-soft whitespace-pre-wrap">{answers[idx].answer}</p>
            </div>
          )}

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 pt-4 border-t border-line">
            {item.strengths && (
              <section>
                <h4 className="text-sm font-semibold mb-2 text-pass">Strengths</h4>
                <p className="text-ink-soft text-sm">{item.strengths}</p>
              </section>
            )}
            <section>
              <h4 className="text-sm font-semibold mb-2 text-warn">Gaps</h4>
              <p className="text-ink-soft text-sm">{item.areas_for_improvement}</p>
            </section>
          </div>
        </div>
      ))}
    </div>
  );
}
