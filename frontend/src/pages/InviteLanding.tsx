import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { Loader2, Briefcase } from 'lucide-react';
import { useAuth } from '../context/AuthContext';
import { saveInvite, clearInvite } from '../lib/invite';

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

  // Which invite this page is actually showing. Opening a second invite link
  // in the same session leaves the first lookup in flight: without this, a
  // slower response for the abandoned link would overwrite the stored token
  // and hand the finished report to the wrong recruiter — and a stale failure
  // would clear a perfectly good invite and show an error over a valid page.
  const currentToken = useRef<string | null>(null);

  useEffect(() => {
    if (!token) return;

    // Two guards, because they catch different things. The ref rejects a
    // response for an invite the page has moved on from; `active` rejects one
    // that arrives after this lookup was abandoned entirely — the ref lives on
    // the component, so on unmount it still matches and a late success would
    // quietly persist an invite the candidate navigated away from before it
    // even rendered.
    let active = true;
    currentToken.current = token;

    // Nothing from the previous invite may survive into this one: the old
    // invitation would stay on screen while the new one loads, and because the
    // error branch renders first, an error left over from a dead link would
    // keep showing "Invite unavailable" over a perfectly good invite.
    setInvite(null);
    setError('');

    // Drop whatever was held before looking this one up. Opening a second
    // invite link, or opening one that turns out to be dead, must not leave
    // the previous recruiter's token in storage for a later interview to pick
    // up.
    clearInvite();

    fetch(`${import.meta.env.VITE_API_URL}/api/public/invites/${encodeURIComponent(token)}`)
      .then(async res => {
        const data = await res.json().catch(() => null);
        if (!res.ok) throw new Error(data?.error || 'This invite link is not valid.');
        return data;
      })
      .then((data: Invite) => {
        if (!active || currentToken.current !== token) return;
        setInvite(data);
        // Held until the interview is submitted, which is what links the
        // finished report back to the recruiter who asked for it. Expires on
        // its own so an abandoned invite cannot attach to a later run.
        saveInvite(token, data.role_title);
      })
      .catch(err => {
        if (!active || currentToken.current !== token) return;
        clearInvite();
        setError(err.message);
      });

    return () => {
      active = false;
      if (currentToken.current === token) currentToken.current = null;
    };
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
