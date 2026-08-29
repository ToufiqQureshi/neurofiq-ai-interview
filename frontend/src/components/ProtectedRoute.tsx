import { Navigate, Outlet } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { Loader2 } from 'lucide-react';

export function ProtectedRoute() {
  const { isAuthenticated, loading } = useAuth();

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-paper">
        <Loader2 className="w-8 h-8 text-accent animate-spin" />
      </div>
    );
  }

  // If the API checked and you are not authenticated, redirect to /auth
  if (!isAuthenticated) {
    return <Navigate to="/auth" replace />;
  }

  // Otherwise, render the child routes (e.g. DashboardLayout)
  return <Outlet />;
}
