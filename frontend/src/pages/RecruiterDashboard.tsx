import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Loader2, Plus, Link2, Check, Ban, Users } from 'lucide-react';

type Invite = {
  id: string;
  token: string;
  role_title: string;
  note: string;
  max_uses: number;
  uses: number;
  expires_at: string;
  revoked_at?: string | null;
};

type Candidate = {
  session_id: string;
  role_title: string;
  github_username: string;
  avatar_url: string;
  repo_full_name: string;
  overall_score: number;
  mode: string;
  completed_at: string;
};

const api = import.meta.env.VITE_API_URL;

// RecruiterDashboard is the hiring side of the same product: send the
// repo-based interview to your applicants, get them back ranked.
export function RecruiterDashboard() {
  const [invites, setInvites] = useState<Invite[]>([]);
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [creating, setCreating] = useState(false);
  const [roleTitle, setRoleTitle] = useState('');
  const [note, setNote] = useState('');
  const [maxUses, setMaxUses] = useState(1);
  const [copiedToken, setCopiedToken] = useState('');

  const load = async () => {
    try {
      const [invitesRes, candidatesRes] = await Promise.all([
        fetch(`${api}/api/recruiter/invites`, { credentials: 'include' }),
        fetch(`${api}/api/recruiter/candidates`, { credentials: 'include' }),
      ]);
      if (invitesRes.status === 403 || candidatesRes.status === 403) {
        throw new Error('This area is for hiring accounts. Ask us to enable recruiter access on your account.');
      }
      if (!invitesRes.ok || !candidatesRes.ok) throw new Error('Could not load your hiring dashboard.');
      const invitesData = await invitesRes.json();
      const candidatesData = await candidatesRes.json();
      setInvites(invitesData.invites || []);
      setCandidates(candidatesData.candidates || []);
    } catch (err: any) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const createInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setError('');
    try {
      const res = await fetch(`${api}/api/recruiter/invites`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ role_title: roleTitle, note, max_uses: maxUses, expires_in_days: 30 }),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) throw new Error(data?.error || 'Could not create the invite.');
      setInvites(prev => [data.invite, ...prev]);
      setRoleTitle('');
      setNote('');
    } catch (err: any) {
      setError(err.message);
    } finally {
      setCreating(false);
    }
  };

  const revoke = async (id: string) => {
    const res = await fetch(`${api}/api/recruiter/invites/${id}/revoke`, {
      method: 'POST',
      credentials: 'include',
    });
    if (res.ok) load();
  };

  const copyInvite = async (token: string) => {
    try {
      await navigator.clipboard.writeText(`${window.location.origin}/invite/${token}`);
      setCopiedToken(token);
      setTimeout(() => setCopiedToken(''), 2000);
    } catch {
      // Clipboard can be blocked; the link is shown in full below.
    }
  };

  if (loading) {
    return (
      <div className="p-8 flex justify-center">
        <Loader2 className="w-8 h-8 text-accent animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 md:p-8 max-w-7xl mx-auto space-y-5">
      <div>
        <h1 className="font-display text-2xl font-extrabold text-ink">Candidates</h1>
        <p className="text-sm text-ink-faint mt-1">
          Send the repo-based interview to your applicants and read them ranked by how they reasoned about their own code.
        </p>
      </div>

      {error && (
        <div className="bg-crit-soft border border-crit/20 text-crit rounded-xl p-4 text-sm">{error}</div>
      )}

      <form onSubmit={createInvite} className="bg-surface border border-line rounded-xl p-5 space-y-4">
        <h2 className="font-display font-bold text-ink text-base">New interview link</h2>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          <input
            required
            value={roleTitle}
            onChange={e => setRoleTitle(e.target.value)}
            placeholder="Role, e.g. Senior Backend Engineer"
            className="md:col-span-2 bg-paper border border-line rounded-lg px-3 py-2 text-sm outline-none focus:border-accent"
          />
          <input
            type="number"
            min={0}
            max={500}
            value={maxUses}
            onChange={e => setMaxUses(Number(e.target.value))}
            title="How many candidates may use this link. 0 means unlimited."
            className="bg-paper border border-line rounded-lg px-3 py-2 text-sm outline-none focus:border-accent tabular-nums"
          />
        </div>
        <textarea
          value={note}
          onChange={e => setNote(e.target.value)}
          placeholder="A line the candidate sees — what the role is, what you're looking for."
          className="w-full bg-paper border border-line rounded-lg px-3 py-2 text-sm outline-none focus:border-accent resize-none min-h-[64px]"
        />
        <button
          type="submit"
          disabled={creating}
          className="bg-ink hover:bg-black text-white font-semibold text-sm px-5 py-2.5 rounded-full transition-colors flex items-center gap-2 disabled:opacity-50"
        >
          <Plus className="w-4 h-4" />
          {creating ? 'Creating…' : 'Create link'}
        </button>
      </form>

      {invites.length > 0 && (
        <div className="bg-surface border border-line rounded-xl overflow-hidden">
          <div className="p-5 border-b border-line">
            <h2 className="font-display font-bold text-ink text-base">Your links</h2>
          </div>
          <div className="divide-y divide-line">
            {invites.map(invite => {
              const revoked = !!invite.revoked_at;
              const spent = invite.max_uses > 0 && invite.uses >= invite.max_uses;
              return (
                <div key={invite.id} className="p-5 flex items-center justify-between gap-4 flex-wrap">
                  <div className="min-w-0">
                    <p className="font-semibold text-sm text-ink">{invite.role_title}</p>
                    <p className="text-xs text-ink-faint font-mono mt-1 truncate">
                      /invite/{invite.token}
                    </p>
                    <p className="text-[11px] text-ink-faint mt-1 tabular-nums">
                      {invite.uses} used{invite.max_uses > 0 ? ` of ${invite.max_uses}` : ' · unlimited'}
                      {revoked && ' · revoked'}
                      {!revoked && spent && ' · fully used'}
                    </p>
                  </div>
                  <div className="flex gap-2">
                    <button
                      onClick={() => copyInvite(invite.token)}
                      className="px-4 py-2 rounded-full border border-line-strong text-xs font-semibold text-ink hover:bg-paper transition-colors flex items-center gap-2"
                    >
                      {copiedToken === invite.token ? <Check className="w-3.5 h-3.5 text-pass" /> : <Link2 className="w-3.5 h-3.5" />}
                      {copiedToken === invite.token ? 'Copied' : 'Copy link'}
                    </button>
                    {!revoked && (
                      <button
                        onClick={() => revoke(invite.id)}
                        className="px-4 py-2 rounded-full border border-line-strong text-xs font-semibold text-crit hover:bg-crit-soft transition-colors flex items-center gap-2"
                      >
                        <Ban className="w-3.5 h-3.5" />
                        Revoke
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      <div className="bg-surface border border-line rounded-xl overflow-hidden">
        <div className="flex justify-between items-center p-5 border-b border-line">
          <h2 className="font-display font-bold text-ink text-base">Ranked candidates</h2>
          <span className="text-xs font-mono text-ink-faint tabular-nums">{candidates.length}</span>
        </div>
        {candidates.length === 0 ? (
          <div className="p-10 text-center space-y-2">
            <Users className="w-8 h-8 text-ink-faint mx-auto" />
            <p className="text-sm text-ink-faint">
              No completed interviews yet. Share a link above and they'll appear here, best score first.
            </p>
          </div>
        ) : (
          <div className="divide-y divide-line">
            {candidates.map(candidate => {
              const score = candidate.overall_score ?? 0;
              const band = score >= 8 ? 'text-pass' : score >= 5 ? 'text-warn' : 'text-crit';
              return (
                <Link
                  key={candidate.session_id}
                  to={`/hiring/report/${candidate.session_id}`}
                  className="p-5 flex items-center gap-4 hover:bg-paper transition-colors"
                >
                  {candidate.avatar_url ? (
                    <img src={candidate.avatar_url} alt={candidate.github_username} className="w-10 h-10 rounded-full object-cover flex-shrink-0" />
                  ) : (
                    <div className="w-10 h-10 rounded-full bg-accent text-white flex items-center justify-center font-mono text-xs font-bold flex-shrink-0">
                      {(candidate.github_username || '??').slice(0, 2).toUpperCase()}
                    </div>
                  )}
                  <div className="flex-1 min-w-0">
                    <p className="font-semibold text-sm text-ink truncate">{candidate.github_username}</p>
                    <p className="text-xs text-ink-faint font-mono truncate">{candidate.repo_full_name}</p>
                    <p className="text-[11px] text-ink-faint mt-0.5">{candidate.role_title}</p>
                  </div>
                  <div className={`font-mono text-2xl font-medium tabular-nums ${band} flex-shrink-0`}>
                    {score.toFixed(1)}
                  </div>
                </Link>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
