import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Loader2, ArrowRight, ShieldCheck, Video } from 'lucide-react';

export function InviteLanding() {
  const { slug } = useParams();
  const navigate = useNavigate();
  const [invite, setInvite] = useState<any>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch(`${import.meta.env.VITE_API_URL}/api/invites/${slug}`)
      .then(r => {
        if (!r.ok) throw new Error('Invalid or expired invite link.');
        return r.json();
      })
      .then(d => {
        setInvite(d.invite);
      })
      .catch(err => {
        setError(err.message);
      })
      .finally(() => setLoading(false));
  }, [slug]);

  if (loading) {
    return (
      <div className="min-h-screen bg-slate-950 flex items-center justify-center">
        <Loader2 className="w-8 h-8 text-accent animate-spin" />
      </div>
    );
  }

  if (error || !invite) {
    return (
      <div className="min-h-screen bg-slate-950 flex flex-col items-center justify-center p-6 text-center">
        <div className="w-16 h-16 rounded-full bg-crit/20 text-crit flex items-center justify-center mb-6">
          <span className="font-mono text-2xl font-bold">X</span>
        </div>
        <h1 className="font-display text-3xl font-extrabold text-white mb-2">Invalid Invite</h1>
        <p className="text-slate-400 mb-8 max-w-md">{error}</p>
        <button onClick={() => navigate('/')} className="px-6 py-2 bg-slate-800 text-white rounded hover:bg-slate-700 font-medium transition-colors">
          Return to Home
        </button>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-950 flex items-center justify-center p-6 relative overflow-hidden">
      {/* Background glow */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[600px] bg-accent/20 rounded-full blur-[120px] pointer-events-none"></div>
      
      <div className="w-full max-w-2xl bg-slate-900/80 backdrop-blur-xl border border-slate-800 p-10 rounded-2xl shadow-2xl relative z-10 flex flex-col items-center text-center">
        <div className="flex items-center gap-2 mb-8">
          <div className="w-4 h-4 rounded-full bg-accent animate-pulse" />
          <span className="font-mono text-sm font-bold uppercase tracking-widest text-slate-300">NeuroFIQ Enterprise</span>
        </div>
        
        <h1 className="font-display text-4xl font-extrabold text-white mb-4">
          You've been invited to an AI Technical Interview.
        </h1>
        
        <p className="text-lg text-slate-400 mb-10 max-w-lg">
          This is an advanced technical assessment. The AI will evaluate your system design, coding abilities, and problem-solving skills in real-time.
        </p>

        <div className="w-full bg-slate-950 rounded-xl border border-slate-800 p-6 mb-10 text-left">
          <h3 className="font-mono text-xs uppercase tracking-widest text-slate-500 mb-4">Interview Requirements</h3>
          <ul className="space-y-4">
            <li className="flex items-start gap-3">
              <Video className="w-5 h-5 text-accent shrink-0 mt-0.5" />
              <div>
                <p className="text-sm font-semibold text-white">Camera & Microphone Access</p>
                <p className="text-xs text-slate-400 mt-1">This is a proctored voice interview. Your audio will be recorded.</p>
              </div>
            </li>
            <li className="flex items-start gap-3">
              <ShieldCheck className="w-5 h-5 text-accent shrink-0 mt-0.5" />
              <div>
                <p className="text-sm font-semibold text-white">Screen Sharing & Anti-Cheat</p>
                <p className="text-xs text-slate-400 mt-1">You must share your entire screen. Navigating away from the tab will flag your session.</p>
              </div>
            </li>
          </ul>
        </div>

        <button 
          onClick={() => {
            // Drop them into the interview session with the repo assigned, or a generic one if none
            const repoParam = invite.repo_full_name ? encodeURIComponent(invite.repo_full_name) : 'system/general-assessment';
            navigate(`/interview/${repoParam}?mode=voice&token=${invite.token}`);
          }}
          className="w-full flex items-center justify-center gap-3 bg-accent hover:bg-accent-hover text-white text-lg font-bold py-4 px-8 rounded-xl transition-all shadow-[0_0_20px_rgba(59,130,246,0.3)] hover:shadow-[0_0_30px_rgba(59,130,246,0.5)] group"
        >
          <span>Start Technical Interview</span>
          <ArrowRight className="w-5 h-5 group-hover:translate-x-1 transition-transform" />
        </button>
      </div>
    </div>
  );
}
