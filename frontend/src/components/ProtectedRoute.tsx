import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { Loader2 } from 'lucide-react';

export function ProtectedRoute() {
  const { user, isAuthenticated, loading } = useAuth();
  const location = useLocation();

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
