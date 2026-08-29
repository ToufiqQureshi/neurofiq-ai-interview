import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { Loader2, Briefcase } from 'lucide-react';
import { useAuth } from '../context/AuthContext';

type Invite = {
  role_title: string;
  note: string;
  expires_at: string;
  recruiter: { github_username: string; avatar_url: string };
};

// InviteLanding is the candidate's first screen when a company sends them an
// interview link. Reading an invite deliberately does not consume one of its
// uses — that only happens when the interview is actually submitted.
export function InviteLanding() {
  const { token } = useParams();
  const navigate = useNavigate();
  const { user, loading } = useAuth();
  const [invite, setInvite] = useState<Invite | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!token) return;
    fetch(`${import.meta.env.VITE_API_URL}/api/public/invites/${encodeURIComponent(token)}`)
      .then(async res => {
        const data = await res.json().catch(() => null);
        if (!res.ok) throw new Error(data?.error || 'This invite link is not valid.');
        return data;
      })
      .then((data: Invite) => {
        setInvite(data);
        // Held until the interview is submitted, which is what links the
        // finished report back to the recruiter who asked for it.
        sessionStorage.setItem('neurofiq_invite', token);
      })
      .catch(err => setError(err.message));
  }, [token]);

  if (error) {
    return (
      <div className="min-h-screen bg-paper flex flex-col items-center justify-center gap-4 p-8 text-center">
        <h1 className="font-display text-2xl font-bold text-ink">Invite unavailable</h1>
        <p className="text-ink-soft max-w-md">{error}</p>
        <Link to="/" className="text-accent font-semibold hover:underline">Go to NeuroFIQ</Link>
      </div>
    );
  }

  if (!invite || loading) {
    return (
      <div className="min-h-screen bg-paper flex items-center justify-center">
        <Loader2 className="w-10 h-10 text-accent animate-spin" />
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-paper text-ink flex items-center justify-center p-6">
      <div className="max-w-lg w-full bg-surface border border-line rounded-2xl p-8 space-y-6">
        <div className="flex items-center gap-3">
          <span className="w-10 h-10 rounded-xl bg-accent-soft flex items-center justify-center">
            <Briefcase className="w-5 h-5 text-accent" />
          </span>
          <div>
            <div className="text-[10px] font-mono uppercase tracking-widest text-ink-faint">
              Interview invitation
            </div>
            <h1 className="font-display text-xl font-extrabold">{invite.role_title}</h1>
          </div>
        </div>

        {invite.note && (
          <p className="text-sm text-ink-soft leading-relaxed bg-paper border border-line rounded-xl p-4">
            {invite.note}
          </p>
        )}

        <div className="space-y-2 text-sm text-ink-soft">
          <p className="font-semibold text-ink">How this works</p>
          <p>
            You pick one of your own repositories. NeuroFIQ reads the code and the
            commit history, then asks five questions about the decisions you made in it.
            Your answers are scored and sent to {invite.recruiter?.github_username || 'the hiring team'}.
          </p>
          <p className="text-ink-faint">Takes about 15 minutes. No LeetCode.</p>
        </div>

        {user ? (
          <button
            onClick={() => navigate('/repositories')}
            className="w-full bg-ink hover:bg-black text-white py-3 rounded-full font-semibold transition-colors"
          >
            Pick a repository and start
          </button>
        ) : (
          <button
            onClick={() => {
              const api = import.meta.env.VITE_API_URL;
              if (!api) {
                setError('VITE_API_URL is not set. Check the frontend .env.');
                return;
              }
              window.location.href = `${api}/auth/github/login`;
            }}
            className="w-full bg-ink hover:bg-black text-white py-3 rounded-full font-semibold transition-colors"
          >
            Sign in with GitHub to start
          </button>
        )}
      </div>
    </div>
  );
}
