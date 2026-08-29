import { useEffect, useState } from 'react';
import { Loader2, Code2, Server, Database, ArrowRight, AlertTriangle } from 'lucide-react';
import { Link, useParams, useNavigate } from 'react-router-dom';

export function AnalysisProgress() {
  const { repoId } = useParams();
  const navigate = useNavigate();
  const [step, setStep] = useState(0);
  const [analysis, setAnalysis] = useState<any>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!repoId) return;

    const repoFullName = decodeURIComponent(repoId);
    setStep(0); // Fetching file structure...

    const startAnalysis = async () => {
      try {
        const res = await fetch(`${import.meta.env.VITE_API_URL}/api/repos/analyze`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'include',
          body: JSON.stringify({ repo_full_name: repoFullName })
        });

        if (res.status === 401) {
          navigate('/auth');
          return;
        }

        if (res.status === 403) {
          const data = await res.json();
          throw new Error(data.error || 'Free tier limit reached.');
        }

        if (!res.ok) throw new Error('Analysis failed');

        const data = await res.json();

        if (data.status === 'completed') {
           setStep(2);
           if (data.analysis) {
             const parsedAnalysis = typeof data.analysis === 'string' ? JSON.parse(data.analysis) : data.analysis;
             setAnalysis(parsedAnalysis);
             setTimeout(() => setStep(3), 1000);
           }
        } else if (data.status === 'processing') {
           setStep(1); // Polling state
           pollStatus();
        }
      } catch (err: any) {
        console.error(err);
        setError(err.message);
      }
    };

    let interval: ReturnType<typeof setInterval>;
    const pollStatus = () => {
       interval = setInterval(async () => {
         try {
           const res = await fetch(`${import.meta.env.VITE_API_URL}/api/repos/analyze/status?repo=${encodeURIComponent(repoFullName)}`, {
             credentials: 'include'
           });

           if (!res.ok) throw new Error("Failed to check status");
           const data = await res.json();

           if (data.status === 'completed') {
             clearInterval(interval);
             setStep(2);
             if (data.analysis) {
               const parsedAnalysis = typeof data.analysis === 'string' ? JSON.parse(data.analysis) : data.analysis;
               setAnalysis(parsedAnalysis);
               setTimeout(() => setStep(3), 1000);
             }
           }
         } catch (err: any) {
           clearInterval(interval);
           setError(err.message);
         }
       }, 3000); // Poll every 3 seconds
    };

    startAnalysis();
    return () => {
      if (interval) clearInterval(interval);
    };
  }, [repoId, navigate]);

  const steps = [
    "Fetching file structure...",
    "Detecting tech stack...",
    "Analyzing architecture & complexity...",
    "Ready for Interview!"
  ];

  return (
    <div className="min-h-screen bg-paper text-ink flex items-center justify-center p-4">
      <div className="max-w-2xl w-full bg-surface rounded-2xl p-8 border border-line shadow-sm">

        {error ? (
          <div className="text-center space-y-6">
            <AlertTriangle className="w-12 h-12 text-crit mx-auto" />
            <h2 className="font-display text-2xl font-semibold text-crit">Analysis Failed</h2>
            <p className="text-ink-soft">{error}</p>
            <Link to="/dashboard" className="inline-block mt-4 text-accent hover:underline">Back to Dashboard</Link>
          </div>
        ) : step < 3 ? (
          <div className="text-center space-y-6">
            <Loader2 className="w-12 h-12 text-accent animate-spin mx-auto" />
            <h2 className="font-display text-2xl font-semibold">{steps[step]}</h2>
            <div className="w-full bg-line h-2 rounded-full overflow-hidden">
              <div
                className="bg-accent h-full transition-all duration-500"
                style={{ width: `${(step / 3) * 100}%` }}
              ></div>
            </div>
          </div>
        ) : (
          <div className="space-y-8">
            <div className="text-center">
              <div className="w-16 h-16 bg-pass-soft text-pass rounded-full flex items-center justify-center mx-auto mb-4">
                <Code2 className="w-8 h-8" />
              </div>
              <h2 className="font-display text-3xl font-bold mb-2 text-ink">Analysis Complete</h2>
              <p className="text-ink-faint">{decodeURIComponent(repoId || '')}</p>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="bg-paper p-4 rounded-xl border border-line">
                <div className="flex items-center gap-2 mb-2 text-ink-faint text-xs font-mono uppercase tracking-wider">
                  <Server className="w-4 h-4" /> Patterns Detected
                </div>
                <div className="font-semibold text-lg flex flex-wrap gap-2">
                  {analysis?.architecture_patterns?.slice(0, 2).map((p: string, i: number) => (
                    <span key={i} className="text-accent bg-accent-soft px-2 py-1 rounded text-sm">{p}</span>
                  )) || <span className="text-ink-faint text-sm">None</span>}
                </div>
              </div>
              <div className="bg-paper p-4 rounded-xl border border-line">
                <div className="flex items-center gap-2 mb-2 text-ink-faint text-xs font-mono uppercase tracking-wider">
                  <Database className="w-4 h-4" /> Complexity
                </div>
                {(() => {
                  const raw = analysis?.overall_complexity || "Unknown";
                  // Older cached analyses stored a full sentence here instead of a short label.
                  const label = raw.split(/[:.]/)[0].trim();
                  return (
                    <>
                      <div className="font-semibold text-lg text-warn">{label}</div>
                      {analysis?.complexity_reasoning && (
                        <p className="text-xs text-ink-faint mt-1 leading-relaxed">{analysis.complexity_reasoning}</p>
                      )}
                    </>
                  );
                })()}
              </div>
            </div>

            <div className="pt-4 flex gap-3">
              <Link to={`/interview/${encodeURIComponent(repoId || '')}?mode=text`} className="flex-1 bg-surface border border-line-strong hover:bg-paper text-ink py-3 rounded-full font-semibold transition-colors text-center">
                Start Text Interview
              </Link>
              <Link to={`/interview/${encodeURIComponent(repoId || '')}?mode=voice`} className="flex-1 bg-ink hover:bg-black text-white py-3 rounded-full font-semibold transition-colors flex items-center justify-center gap-2">
                Start Voice Interview <ArrowRight className="w-4 h-4" />
              </Link>
            </div>
          </div>
        )}

      </div>
    </div>
  );
}
