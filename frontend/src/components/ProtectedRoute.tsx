import { useEffect } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { Loader2 } from 'lucide-react';
import { rememberReturnTo, consumeReturnTo } from '../lib/interviewTarget';

export function ProtectedRoute() {
  const { user, isAuthenticated, loading } = useAuth();
  const location = useLocation();

  // Remembered before the redirect fires, not read back until sign-in lands
  // on /dashboard below — see interviewTarget.ts for why this has to survive
  // in sessionStorage rather than in the URL or in router state.
  useEffect(() => {
    if (!loading && !isAuthenticated) {
      rememberReturnTo(location.pathname + location.search);
    }
  }, [loading, isAuthenticated, location.pathname, location.search]);

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

  // /dashboard is where both sign-in paths land a freshly authenticated user
  // — the password form navigates here directly, and the GitHub OAuth
  // callback runs on the server and can only redirect to a fixed path, never
  // to a Job Map id it never saw. So this is also the one place that can
  // hand a candidate back to the opening they started from, and the one
  // place onboarding's own "finished" redirect lands too.
  if (
    location.pathname === '/dashboard' ||
    (user?.is_onboarded === true && location.pathname === '/onboarding')
  ) {
    const returnTo = consumeReturnTo();
    if (returnTo && returnTo !== location.pathname + location.search) {
      return <Navigate to={returnTo} replace />;
    }
    // If already onboarded and hits /onboarding with nothing to return to,
    // redirect to /dashboard
    if (user?.is_onboarded === true && location.pathname === '/onboarding') {
      return <Navigate to="/dashboard" replace />;
    }
  }

  return <Outlet />;
}
