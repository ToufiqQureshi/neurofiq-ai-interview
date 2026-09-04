import { useEffect, useRef } from 'react';
import { Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { Loader2 } from 'lucide-react';
import { rememberReturnTo, consumeReturnTo } from '../lib/interviewTarget';

export function ProtectedRoute() {
  const { user, isAuthenticated, loading } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();

  // Remembered before the redirect fires, not read back until sign-in lands
  // on /dashboard below — see interviewTarget.ts for why this has to survive
  // in sessionStorage rather than in the URL or in router state.
  useEffect(() => {
    if (!loading && !isAuthenticated) {
      rememberReturnTo(location.pathname + location.search);
    }
  }, [loading, isAuthenticated, location.pathname, location.search]);

  // consumeReturnTo clears sessionStorage as it reads, so it cannot run
  // during render: React's StrictMode double-invokes a component's render
  // body in development, and the first of the two calls would consume the
  // value before the render React actually commits ever saw it — landing on
  // /dashboard with the target silently gone, the exact bug this file exists
  // to fix. consumedFor guards the effect the same way InterviewSession.tsx
  // guards its one-shot question fetch: a ref survives StrictMode's
  // simulated remount, so only the first of the two invocations reads it.
  const consumedFor = useRef<string | null>(null);
  useEffect(() => {
    if (loading || !isAuthenticated || location.pathname !== '/dashboard') return;
    const key = location.pathname + location.search;
    if (consumedFor.current === key) return;
    consumedFor.current = key;

    // /dashboard is where both sign-in paths land a freshly authenticated
    // user — the password form navigates here directly, and the GitHub
    // OAuth callback runs on the server and can only redirect to a fixed
    // path, never to a Job Map id it never saw. So this is also the one
    // place that can hand a candidate back to the opening they started
    // from.
    const returnTo = consumeReturnTo();
    if (returnTo && returnTo !== key) {
      navigate(returnTo, { replace: true });
    }
  }, [loading, isAuthenticated, location.pathname, location.search, navigate]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-paper">
        <Loader2 className="w-8 h-8 text-accent animate-spin" />
      </div>
    );
  }

  // If not authenticated, redirect to /auth
  if (!isAuthenticated) {
    return <Navigate to="/auth" replace />;
  }

  // If new user not yet onboarded, force /onboarding
  if (user?.is_onboarded === false && location.pathname !== '/onboarding') {
    return <Navigate to="/onboarding" replace />;
  }

  // If already onboarded and hits /onboarding, redirect to /dashboard
  if (user?.is_onboarded === true && location.pathname === '/onboarding') {
    return <Navigate to="/dashboard" replace />;
  }

  return <Outlet />;
}
