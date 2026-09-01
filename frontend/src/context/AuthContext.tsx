import { createContext, useContext, useEffect, useState } from 'react';

export interface User {
  id: string;
  github_username?: string;
  full_name?: string;
  email: string;
  avatar_url?: string;
  role?: string;
  plan_type?: string;
  is_onboarded?: boolean;
  experience_level?: string;
  target_role?: string;
  tech_stack?: string;
  linkedin_url?: string;
  college_or_company?: string;
  interview_goal?: string;
  github_connected?: boolean;
}

interface AuthContextType {
  user: User | null;
  isAuthenticated: boolean;
  loading: boolean;
  setUser: React.Dispatch<React.SetStateAction<User | null>>;
  refreshUser: () => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  isAuthenticated: false,
  loading: true,
  setUser: () => {},
  refreshUser: async () => {},
  logout: async () => {},
});

export const useAuth = () => useContext(AuthContext);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  const refreshUser = async () => {
    try {
      const res = await fetch(`${import.meta.env.VITE_API_URL}/auth/me`, { credentials: 'include' });
      if (res.ok) {
        const d = await res.json();
        setUser(d?.user || null);
      } else {
        setUser(null);
      }
    } catch {
      setUser(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    refreshUser();
  }, []);

  const logout = async () => {
    try {
      await fetch(`${import.meta.env.VITE_API_URL}/auth/logout`, {
        method: 'POST',
        credentials: 'include',
      });
    } finally {
      setUser(null);
      window.location.href = '/';
    }
  };

  return (
    <AuthContext.Provider value={{ user, isAuthenticated: !!user, loading, setUser, refreshUser, logout }}>
      {children}
    </AuthContext.Provider>
  );
}
