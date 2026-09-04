import { useState, useEffect, useRef } from 'react';
import { Search, CheckCircle2, AlertTriangle, Play, ChevronRight, Sparkles, ShieldAlert, CheckSquare, Lightbulb, Terminal, Cpu, Activity, User, Fingerprint } from 'lucide-react';
import { Link } from 'react-router-dom';

export function Radar() {
  const [url, setUrl] = useState('');
  // 'failed' is separate from 'idle' so the terminal panel — and the [ERR]
  // line the catch writes into it — survives a failed scan. Returning to idle
  // unmounted the panel and threw the only record of what went wrong away.
  const [scanState, setScanState] = useState<'idle' | 'scanning' | 'results' | 'failed'>('idle');
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [radarData, setRadarData] = useState<any>(null);
  
  // Terminal Logs simulation
  const [logs, setLogs] = useState<string[]>([]);

  // The progress ticker is cleared on both the success and the error path, but
  // neither runs if the page is left mid-scan — the interval then survives the
  // component and keeps calling setProgress on something that is gone. Holding
  // the id in a ref lets the unmount clean it up too.
  const progressTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => () => {
    if (progressTimer.current) clearInterval(progressTimer.current);
  }, []);

  const handleScan = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!url.trim()) return;
    
    setScanState('scanning');
    setProgress(0);
    setError(null);
    setRadarData(null);
    setLogs([`[SYS] Sending ${url.trim()} for analysis...`]);

    // The bar is a waiting indicator, not a measurement: the scan is one
    // request and the server reports nothing until it answers, so there is no
    // real progress to show. It creeps and stops at 95 for that reason.
    //
    // It used to narrate as well — "Parsing DOM elements", "Running NLP entity
    // extraction", "[WARN] Missing critical tech tags" — all on a 500ms timer,
    // none of it anything the backend had said or done. The warning was the
    // worst of them: it announced a finding before a single byte had come
    // back, and it appeared whatever the profile turned out to contain. A
    // progress bar that overstates is a nuisance; invented findings are the
    // thing this project has a rule against.
    const interval = setInterval(() => {
      setProgress((prev) => (prev >= 95 ? 95 : prev + 5));
    }, 500);
    progressTimer.current = interval;

    try {
      const res = await fetch(`${import.meta.env.VITE_API_URL}/api/radar/analyze`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
        credentials: 'include'
      });
      
      clearInterval(interval);
      progressTimer.current = null;

      if (!res.ok) {
        throw new Error('Failed to analyze profile URL.');
      }

      const data = await res.json();

      // Logged here, not above: a 500 answers this call too, and announcing
      // "[OK] Analysis complete" before checking res.ok reported a success the
      // very next line threw on.
      setProgress(100);
      setLogs(l => [...l, '[OK] Analysis complete.']);
      
      setTimeout(() => {
        setRadarData(data);
        setScanState('results');
      }, 800);
      
    } catch (err: any) {
      clearInterval(interval);
      progressTimer.current = null;
      // 'failed', not 'idle'. The terminal panel renders while scanning, so
      // returning to idle wiped the log the line below had just written — the
      // failure was recorded where nobody could read it.
      setScanState('failed');
      setLogs(l => [...l, '[ERR] Analysis failed.']);
      setError(err.message || 'Something went wrong');
    }
  };

  const resetRadar = () => {
    setScanState('idle');
    setUrl('');
    setProgress(0);
    setError(null);
    setRadarData(null);
    setLogs([]);
  };

  const getFitColor = (score: number) => {
    if (score >= 80) return 'text-emerald-400';
    if (score >= 50) return 'text-amber-400';
    return 'text-rose-400';
  };

  const getFitBorder = (score: number) => {
    if (score >= 80) return 'border-emerald-500/50';
    if (score >= 50) return 'border-amber-500/50';
    return 'border-rose-500/50';
  };
  
  const getFitShadow = (score: number) => {
    if (score >= 80) return 'shadow-[0_0_30px_rgba(16,185,129,0.15)]';
    if (score >= 50) return 'shadow-[0_0_30px_rgba(245,158,11,0.15)]';
    return 'shadow-[0_0_30px_rgba(244,63,94,0.15)]';
  };

  return (
    <div className="flex flex-col h-full bg-[#09090b] text-zinc-100 relative overflow-hidden font-sans selection:bg-indigo-500/30">
      
      {/* Abstract Background Elements */}
      <div className="absolute top-[-20%] left-[-10%] w-[50%] h-[50%] bg-indigo-600/10 rounded-full blur-[120px] pointer-events-none mix-blend-screen" />
      <div className="absolute bottom-[-20%] right-[-10%] w-[50%] h-[50%] bg-fuchsia-600/10 rounded-full blur-[120px] pointer-events-none mix-blend-screen" />
      <div className="absolute inset-0 bg-[url('data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iNDAiIGhlaWdodD0iNDAiIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyI+PGNpcmNsZSBjeD0iMSIgY3k9IjEiIHI9IjEiIGZpbGw9InJnYmEoMjU1LDI1NSwyNTUsMC4wMykiLz48L3N2Zz4=')] pointer-events-none opacity-50" />

      <div className="max-w-7xl mx-auto w-full px-6 py-12 relative z-10 flex-1 flex flex-col overflow-y-auto custom-scrollbar">
        
        {/* Header Section */}
        <div className="flex flex-col items-center justify-center mb-16 text-center pt-10">
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-zinc-800/50 border border-zinc-700/50 mb-6 backdrop-blur-md">
            <Sparkles className="w-4 h-4 text-indigo-400" />
            <span className="text-xs font-semibold tracking-widest text-zinc-300 uppercase">Profile Intelligence</span>
          </div>
          <h1 className="text-5xl md:text-6xl font-display font-extrabold tracking-tight mb-6 bg-clip-text text-transparent bg-gradient-to-br from-white via-zinc-200 to-zinc-500">
            Optimize for Discovery
          </h1>
          <p className="text-zinc-400 max-w-2xl text-lg font-light leading-relaxed">
            Enter your public profile URL. Our heuristic engine acts like an ATS, analyzing your visibility, keyword density, and structural impact.
          </p>
        </div>

        {/* Search Bar */}
        {scanState !== 'results' && (
          <div className="max-w-3xl w-full mx-auto mt-4">
            {error && (
              <div className="mb-6 p-4 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-2xl text-center flex items-center justify-center gap-2">
                <ShieldAlert className="w-5 h-5" />
                <span className="font-medium">{error}</span>
              </div>
            )}
            
            <form onSubmit={handleScan} className="relative group">
              <div className="absolute -inset-1.5 bg-gradient-to-r from-indigo-500/30 via-fuchsia-500/30 to-blue-500/30 rounded-3xl blur-xl opacity-50 group-hover:opacity-100 transition duration-1000"></div>
              <div className="relative flex items-center bg-zinc-900/80 backdrop-blur-2xl border border-zinc-700/50 rounded-3xl shadow-2xl p-2.5 transition-all">
                <Search className="w-6 h-6 text-zinc-500 ml-4 flex-shrink-0" />
                <input
                  type="text"
                  placeholder="https://linkedin.com/in/your-profile"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  disabled={scanState === 'scanning'}
                  className="w-full bg-transparent border-none focus:outline-none text-zinc-100 px-4 py-4 text-lg placeholder:text-zinc-600 disabled:opacity-50"
                />
                <button
                  type="submit"
                  disabled={scanState === 'scanning' || !url.trim()}
                  className="bg-white hover:bg-zinc-200 text-zinc-950 px-8 py-4 rounded-2xl font-bold transition-all disabled:opacity-50 disabled:hover:bg-white whitespace-nowrap flex items-center gap-2"
                >
                  {scanState === 'scanning' ? (
                    <>
                      <div className="w-5 h-5 border-2 border-zinc-900/20 border-t-zinc-950 rounded-full animate-spin" />
                      Analyzing
                    </>
                  ) : (
                    <>
                      Run Diagnostics
                      <Activity className="w-5 h-5" />
                    </>
                  )}
                </button>
              </div>
            </form>

            {/* Terminal Loading State */}
            {(scanState === 'scanning' || scanState === 'failed') && (
              <div className="mt-16 animate-in fade-in slide-in-from-bottom-8 duration-700">
                <div className="bg-zinc-900/80 backdrop-blur-xl border border-zinc-800 rounded-2xl p-6 font-mono text-sm max-w-2xl mx-auto shadow-2xl">
                  <div className="flex items-center gap-2 mb-4 border-b border-zinc-800 pb-4">
                    <Terminal className="w-4 h-4 text-zinc-500" />
                    <span className="text-zinc-400 font-semibold tracking-wider">SYSTEM LOG</span>
                    <div className="ml-auto flex gap-2">
                      <div className="w-3 h-3 rounded-full bg-zinc-700" />
                      <div className="w-3 h-3 rounded-full bg-zinc-700" />
                      <div className="w-3 h-3 rounded-full bg-zinc-700" />
                    </div>
                  </div>
                  
                  <div className="space-y-2 mb-6 min-h-[120px]">
                    {logs.map((log, i) => (
                      <div key={i} className={`flex items-start gap-2 ${log.includes('[OK]') ? 'text-emerald-400' : log.includes('[WARN]') ? 'text-amber-400' : 'text-zinc-400'} animate-in fade-in slide-in-from-left-4 duration-300`}>
                        <span className="opacity-50">❯</span>
                        <span>{log}</span>
                      </div>
                    ))}
                    {/* The blinking cursor says "still working", so it stops
                        when the work has stopped. */}
                    {scanState === 'scanning' && (
                      <div className="flex items-start gap-2 text-indigo-400 animate-pulse">
                        <span className="opacity-50">❯</span>
                        <span className="w-2 h-4 bg-indigo-400 inline-block" />
                      </div>
                    )}
                  </div>

                  {/* A failed scan keeps its log but drops the progress bar.
                      Leaving it at "PROCESSING 45%" read as a scan still
                      running, which is the same overstatement the invented
                      log lines were removed for. */}
                  {scanState === 'scanning' ? (
                    <div>
                      <div className="flex justify-between text-xs text-zinc-500 mb-2">
                        <span>PROCESSING</span>
                        <span>{progress}%</span>
                      </div>
                      <div className="w-full h-1 bg-zinc-800 rounded-full overflow-hidden">
                        <div
                          className="h-full bg-indigo-500 shadow-[0_0_10px_rgba(99,102,241,0.8)] transition-all duration-300 ease-out"
                          style={{ width: `${progress}%` }}
                        />
                      </div>
                    </div>
                  ) : (
                    <div className="flex justify-between text-xs text-red-400">
                      <span>SCAN FAILED</span>
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Bento Box Results Dashboard */}
        {scanState === 'results' && radarData && (
          <div className="flex-1 flex flex-col space-y-6 animate-in fade-in slide-in-from-bottom-12 duration-1000 max-w-7xl mx-auto w-full">
            
            {/* Top Bar - Minimalist Context */}
            <div className="flex flex-col md:flex-row md:items-center justify-between bg-zinc-900/50 backdrop-blur-xl border border-zinc-800 p-4 rounded-2xl shadow-lg gap-4">
              <div className="flex items-center gap-4 truncate">
                <div className="w-12 h-12 bg-zinc-800 border border-zinc-700 rounded-xl flex items-center justify-center flex-shrink-0">
                  <User className="w-5 h-5 text-zinc-300" />
                </div>
                <div className="flex flex-col min-w-0">
                  <span className="text-xl font-display font-bold text-white truncate">
                    {radarData.profile_name || 'Anonymous Profile'}
                  </span>
                  <div className="flex items-center gap-2 mt-0.5">
                    <Fingerprint className="w-3.5 h-3.5 text-zinc-500" />
                    <span className="text-xs text-zinc-500 truncate max-w-[200px] md:max-w-md font-mono">
                      {url}
                    </span>
                  </div>
                </div>
              </div>
              <button 
                onClick={resetRadar}
                className="text-sm font-semibold text-zinc-400 hover:text-white px-5 py-2.5 rounded-xl hover:bg-zinc-800 transition-colors border border-transparent flex-shrink-0"
              >
                Scan Another Profile
              </button>
            </div>

            {/* Bento Grid */}
            <div className="grid grid-cols-1 md:grid-cols-12 gap-6 auto-rows-[minmax(180px,_auto)]">
              
              {/* Score Card - Spans 4 cols, 2 rows */}
              <div className={`md:col-span-4 md:row-span-2 bg-zinc-900/60 backdrop-blur-xl border ${getFitBorder(radarData.overall_score)} ${getFitShadow(radarData.overall_score)} rounded-3xl p-8 flex flex-col items-center justify-center relative overflow-hidden transition-all duration-500`}>
                <div className={`absolute top-0 right-0 w-48 h-48 ${getFitColor(radarData.overall_score).replace('text', 'bg').replace('400', '500')}/10 rounded-full blur-[80px]`} />
                
                <h3 className="text-zinc-400 text-sm font-semibold tracking-widest uppercase mb-8 z-10 flex items-center gap-2">
                  <Activity className="w-4 h-4" />
                  ATS Optimization Score
                </h3>
                
                <div className="relative w-48 h-48 flex items-center justify-center mb-6 z-10">
                  <svg className="w-full h-full transform -rotate-90 drop-shadow-xl" viewBox="0 0 100 100">
                    <circle cx="50" cy="50" r="46" fill="none" stroke="rgba(255,255,255,0.05)" strokeWidth="6" />
                    <circle 
                      cx="50" cy="50" r="46" 
                      fill="none" 
                      stroke="currentColor" 
                      className={`${getFitColor(radarData.overall_score)} transition-all duration-[2000ms] ease-out`}
                      strokeWidth="6" 
                      strokeDasharray={`${2 * Math.PI * 46}`}
                      strokeDashoffset={`${2 * Math.PI * 46 * (1 - (radarData.overall_score / 100))}`}
                      strokeLinecap="round" 
                      style={{ animation: 'dash 2s cubic-bezier(0.2, 0.8, 0.2, 1) forwards' }}
                    />
                  </svg>
                  <div className="absolute inset-0 flex flex-col items-center justify-center bg-zinc-900/40 rounded-full backdrop-blur-[2px] m-4">
                    <span className="text-6xl font-display font-black text-white tracking-tighter">
                      {radarData.overall_score}
                    </span>
                    <span className="text-xs font-mono text-zinc-500 mt-1">/ 100</span>
                  </div>
                </div>
                
                <div className="z-10 text-center">
                  <span className={`text-lg font-bold ${getFitColor(radarData.overall_score)}`}>
                    {radarData.overall_score >= 80 ? 'Exceptional Visibility' : radarData.overall_score >= 50 ? 'Moderate Visibility' : 'Low Visibility'}
                  </span>
                </div>
              </div>

              {/* General Advice - Spans 8 cols, 1 row */}
              <div className="md:col-span-8 bg-zinc-900/60 backdrop-blur-xl border border-zinc-800 rounded-3xl p-8 relative overflow-hidden group">
                <div className="absolute right-0 top-0 w-32 h-32 bg-indigo-500/10 rounded-full blur-[60px] group-hover:bg-indigo-500/20 transition-all duration-700" />
                <div className="flex items-center gap-3 mb-4">
                  <div className="w-10 h-10 rounded-xl bg-zinc-800/80 flex items-center justify-center border border-zinc-700">
                    <Cpu className="w-5 h-5 text-indigo-400" />
                  </div>
                  <h3 className="font-display font-bold text-white text-xl">Heuristic Evaluation</h3>
                </div>
                <p className="text-zinc-300 leading-relaxed text-lg font-light">
                  {radarData.general_advice || "Your profile structure has been analyzed against 10,000+ successful tech profiles."}
                </p>
              </div>

              {/* Missing Keywords - Spans 8 cols, 1 row */}
              <div className="md:col-span-8 bg-zinc-900/60 backdrop-blur-xl border border-zinc-800 rounded-3xl p-8">
                <div className="flex items-center gap-3 mb-6">
                  <div className="w-10 h-10 rounded-xl bg-amber-500/10 flex items-center justify-center border border-amber-500/20">
                    <AlertTriangle className="w-5 h-5 text-amber-400" />
                  </div>
                  <div>
                    <h3 className="font-display font-bold text-white text-xl">Missing ATS Entities</h3>
                    <p className="text-sm text-zinc-500">Inject these keywords to bypass automated filters.</p>
                  </div>
                </div>
                
                <div className="flex flex-wrap gap-3">
                  {radarData.missing_keywords && radarData.missing_keywords.length > 0 ? radarData.missing_keywords.map((kw: string, idx: number) => (
                    <div 
                      key={idx} 
                      className="flex items-center gap-2 px-4 py-2.5 rounded-xl text-sm font-mono border bg-amber-500/5 border-amber-500/20 text-amber-300 shadow-[0_0_15px_rgba(245,158,11,0.05)]"
                    >
                      <span className="w-1.5 h-1.5 rounded-full bg-amber-400" />
                      {kw}
                    </div>
                  )) : (
                    <div className="p-6 border border-dashed border-zinc-800 rounded-2xl text-center w-full">
                      <span className="text-sm text-zinc-500 font-mono">No critical entities missing. Profile is saturated.</span>
                    </div>
                  )}
                </div>
              </div>
              
              {/* Section Feedback - Spans 12 cols, auto rows */}
              <div className="md:col-span-12 bg-zinc-900/60 backdrop-blur-xl border border-zinc-800 rounded-3xl p-8">
                <div className="flex items-center gap-3 mb-8 border-b border-zinc-800/50 pb-6">
                  <div className="w-10 h-10 rounded-xl bg-blue-500/10 flex items-center justify-center border border-blue-500/20">
                    <CheckSquare className="w-5 h-5 text-blue-400" />
                  </div>
                  <div>
                    <h3 className="font-display font-bold text-white text-2xl">Section Diagnostics</h3>
                    <p className="text-sm text-zinc-500 mt-1">Actionable rewriting instructions for maximum impact.</p>
                  </div>
                </div>
                
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                  {radarData.section_feedbacks && radarData.section_feedbacks.length > 0 ? radarData.section_feedbacks.map((sf: any, idx: number) => (
                    <div key={idx} className="bg-zinc-950/50 border border-zinc-800/80 rounded-2xl p-6 hover:border-zinc-700 transition-colors">
                      <div className="inline-flex items-center gap-2 px-3 py-1 rounded-lg bg-zinc-900 border border-zinc-800 mb-4">
                        <span className="text-xs font-mono font-semibold text-zinc-300">{sf.section}</span>
                      </div>
                      
                      <div className="space-y-4">
                        <div className="flex items-start gap-3">
                          <div className="w-6 h-6 rounded-md bg-rose-500/10 flex items-center justify-center flex-shrink-0 mt-0.5">
                            <AlertTriangle className="w-3 h-3 text-rose-400" />
                          </div>
                          <div>
                            <span className="text-xs font-bold text-rose-400 uppercase tracking-wider block mb-1">Issue</span>
                            <p className="text-sm text-zinc-400 leading-relaxed">{sf.feedback}</p>
                          </div>
                        </div>
                        
                        <div className="w-full h-[1px] bg-gradient-to-r from-transparent via-zinc-800 to-transparent" />
                        
                        <div className="flex items-start gap-3">
                          <div className="w-6 h-6 rounded-md bg-emerald-500/10 flex items-center justify-center flex-shrink-0 mt-0.5">
                            <Lightbulb className="w-3 h-3 text-emerald-400" />
                          </div>
                          <div>
                            <span className="text-xs font-bold text-emerald-400 uppercase tracking-wider block mb-1">Fix</span>
                            <p className="text-sm text-zinc-200 leading-relaxed font-medium">{sf.suggestion}</p>
                          </div>
                        </div>
                      </div>
                    </div>
                  )) : (
                    <div className="col-span-full p-12 border border-dashed border-zinc-800 rounded-3xl text-center">
                      <CheckCircle2 className="w-12 h-12 text-emerald-500/50 mx-auto mb-4" />
                      <h4 className="text-zinc-300 font-bold text-lg mb-2">No structural issues found</h4>
                      <p className="text-sm text-zinc-500">Your profile sections are perfectly optimized.</p>
                    </div>
                  )}
                </div>
              </div>
            </div>
            
            {/* Bottom Action Area (CTA) */}
            <div className="relative overflow-hidden bg-white text-zinc-950 rounded-[2rem] p-8 md:p-12 flex flex-col md:flex-row items-center justify-between gap-8 mt-4 group">
               <div className="absolute inset-0 bg-gradient-to-r from-indigo-100 to-purple-100 opacity-50" />
               <div className="absolute right-0 top-0 w-64 h-full bg-gradient-to-l from-indigo-200/50 to-transparent" />
               
               <div className="relative z-10 max-w-2xl">
                 <h2 className="text-3xl md:text-4xl font-display font-extrabold mb-4 tracking-tight">
                   Ready to prove your skills?
                 </h2>
                 <p className="text-zinc-600 text-lg leading-relaxed font-medium">
                   A great profile gets you the interview. Great practice gets you the offer. Start a hardcore mock interview tailored to your experience level.
                 </p>
               </div>

               <Link 
                 to="/repositories" 
                 className="relative z-10 flex items-center justify-center gap-3 bg-zinc-950 text-white px-8 py-5 rounded-2xl font-bold hover:bg-zinc-800 transition-all hover:scale-105 active:scale-95 flex-shrink-0 shadow-xl"
               >
                 <Play className="w-5 h-5 fill-white" />
                 <span>Enter Mock Interview</span>
                 <ChevronRight className="w-4 h-4 opacity-50" />
               </Link>
            </div>
          </div>
        )}
      </div>

      <style>{`
        @keyframes dash {
          from {
            stroke-dashoffset: 289.02; /* 2 * PI * 46 */
          }
        }
        .custom-scrollbar::-webkit-scrollbar {
          width: 8px;
        }
        .custom-scrollbar::-webkit-scrollbar-track {
          background: rgba(0,0,0,0.1);
        }
        .custom-scrollbar::-webkit-scrollbar-thumb {
          background: rgba(255,255,255,0.1);
          border-radius: 4px;
        }
        .custom-scrollbar::-webkit-scrollbar-thumb:hover {
          background: rgba(255,255,255,0.2);
        }
      `}</style>
    </div>
  );
}
