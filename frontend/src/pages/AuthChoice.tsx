import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { GithubIcon } from '../components/GithubIcon';
import { useAuth } from '../context/AuthContext';
import { Loader2, Mail, Lock, User as UserIcon, ArrowRight } from 'lucide-react';

export function AuthChoice() {
  const [mode, setMode] = useState<'signin' | 'signup'>('signin');
  const [fullName, setFullName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const { refreshUser } = useAuth();
  const navigate = useNavigate();
  const api = import.meta.env.VITE_API_URL;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!api) {
      setError('VITE_API_URL is not set. Check frontend configuration.');
      return;
    }

    if (mode === 'signup' && !fullName.trim()) {
      setError('Please enter your full name');
      return;
    }

    if (password.length < 8) {
      setError('Password must be at least 8 characters');
      return;
    }

    setLoading(true);
    try {
      const endpoint = mode === 'signup' ? `${api}/auth/register` : `${api}/auth/login`;
      const payload = mode === 'signup' 
        ? { full_name: fullName, email, password }
        : { email, password };

      const res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(payload),
      });

      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.error || 'Authentication failed');
      }

      await refreshUser();

      // Check onboarding state
      if (data?.user?.is_onboarded === false || mode === 'signup') {
        navigate('/onboarding');
      } else {
        navigate('/dashboard');
      }
    } catch (err: any) {
      setError(err.message || 'Something went wrong. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  const handleGithubLogin = () => {
    if (!api) {
      alert('VITE_API_URL is not set. Check the frontend .env.');
      return;
    }
    window.location.href = `${api}/auth/github/login`;
  };

  return (
    <div className="min-h-screen bg-paper flex items-center justify-center p-4">
      <div className="bg-surface p-8 rounded-2xl shadow-sm w-full max-w-md border border-line">
        {/* Brand Header */}
        <div className="text-center mb-6">
          <div className="inline-flex items-center justify-center w-10 h-10 rounded-xl bg-ink text-white font-display font-bold text-lg mb-3">
            N
          </div>
          <h2 className="font-display text-2xl font-extrabold text-ink">
            {mode === 'signin' ? 'Welcome Back' : 'Create Your Account'}
          </h2>
          <p className="text-ink-soft text-xs mt-1">
            {mode === 'signin' 
              ? 'Sign in to access your technical interviews and reports' 
              : 'Join Neurofiq to prepare for technical interviews with AI'}
          </p>
        </div>

        {/* Tab Switcher */}
        <div className="flex bg-paper border border-line p-1 rounded-xl mb-6">
          <button
            type="button"
            onClick={() => { setMode('signin'); setError(null); }}
            className={`flex-1 py-1.5 text-xs font-semibold rounded-lg transition-colors ${
              mode === 'signin'
                ? 'bg-surface text-ink shadow-sm border border-line'
                : 'text-ink-soft hover:text-ink'
            }`}
          >
            Sign In
          </button>
          <button
            type="button"
            onClick={() => { setMode('signup'); setError(null); }}
            className={`flex-1 py-1.5 text-xs font-semibold rounded-lg transition-colors ${
              mode === 'signup'
                ? 'bg-surface text-ink shadow-sm border border-line'
                : 'text-ink-soft hover:text-ink'
            }`}
          >
            Create Account
          </button>
        </div>

        {error && (
          <div className="mb-4 p-3 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 rounded-xl text-xs text-red-600 dark:text-red-400">
            {error}
          </div>
        )}

        {/* Email/Password Form */}
        <form onSubmit={handleSubmit} className="space-y-3 mb-6">
          {mode === 'signup' && (
            <div>
              <label className="block text-xs font-semibold text-ink-soft mb-1">Full Name</label>
              <div className="relative">
                <UserIcon className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-ink-faint" />
                <input
                  type="text"
                  required
                  placeholder="Satya Nadella"
                  value={fullName}
                  onChange={(e) => setFullName(e.target.value)}
                  className="w-full pl-10 pr-4 py-2.5 bg-paper border border-line rounded-xl text-sm text-ink placeholder:text-ink-faint focus:outline-none focus:border-ink transition-colors"
                />
              </div>
            </div>
          )}

          <div>
            <label className="block text-xs font-semibold text-ink-soft mb-1">Email Address</label>
            <div className="relative">
              <Mail className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-ink-faint" />
              <input
                type="email"
                required
                placeholder="you@company.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="w-full pl-10 pr-4 py-2.5 bg-paper border border-line rounded-xl text-sm text-ink placeholder:text-ink-faint focus:outline-none focus:border-ink transition-colors"
              />
            </div>
          </div>

          <div>
            <label className="block text-xs font-semibold text-ink-soft mb-1">Password</label>
            <div className="relative">
              <Lock className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-ink-faint" />
              <input
                type="password"
                required
                placeholder="Min. 8 characters"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="w-full pl-10 pr-4 py-2.5 bg-paper border border-line rounded-xl text-sm text-ink placeholder:text-ink-faint focus:outline-none focus:border-ink transition-colors"
              />
            </div>
          </div>

          <button
            type="submit"
            disabled={loading}
            className="w-full mt-2 bg-ink hover:bg-black text-white flex items-center justify-center gap-2 py-2.5 px-4 rounded-xl font-semibold text-sm transition-colors disabled:opacity-60"
          >
            {loading ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <>
                <span>{mode === 'signin' ? 'Sign In' : 'Create Account'}</span>
                <ArrowRight className="w-4 h-4" />
              </>
            )}
          </button>
        </form>

        {/* Divider */}
        <div className="relative flex py-2 items-center mb-6">
          <div className="flex-grow border-t border-line"></div>
          <span className="flex-shrink-0 mx-3 text-ink-faint text-xs uppercase tracking-wider font-mono">or continue with</span>
          <div className="flex-grow border-t border-line"></div>
        </div>

        {/* Social Logins */}
        <div className="space-y-2.5">
          <button
            type="button"
            onClick={handleGithubLogin}
            className="w-full bg-paper hover:bg-surface text-ink border border-line flex items-center justify-center gap-2.5 py-2.5 px-4 rounded-xl font-semibold text-xs transition-colors"
          >
            <GithubIcon className="w-4 h-4" />
            <span>Continue with GitHub</span>
          </button>

          <button
            type="button"
            disabled
            title="Google sign-in is coming soon"
            className="w-full bg-paper text-ink-faint border border-line flex items-center justify-center gap-2.5 py-2.5 px-4 rounded-xl font-semibold text-xs cursor-not-allowed opacity-75"
          >
            <img src="https://www.svgrepo.com/show/475656/google-color.svg" alt="" className="w-4 h-4 opacity-50 grayscale" />
            <span>Continue with Google</span>
            <span className="ml-1 text-[9px] font-mono uppercase tracking-wider text-ink-faint border border-line rounded-md px-1.5 py-0.5">
              Soon
            </span>
          </button>
        </div>
      </div>
    </div>
  );
}
