import { createContext, useContext, useEffect, useState } from 'react';

interface User {
  id: number;
  github_username: string;
  email: string;
  avatar_url: string;
}

interface AuthContextType {
  user: User | null;
  loading: boolean;
  isAuthenticated: boolean;
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  loading: true,
  isAuthenticated: false,
});

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    // Check if the user's session is still valid
    fetch(`${import.meta.env.VITE_API_URL}/auth/me`, { credentials: 'include' })
      .then(r => r.ok ? r.json() : null)
      .then(d => setUser(d?.user || null))
      .catch(() => setUser(null))
      .finally(() => setLoading(false));
  }, []);

  return (
    <AuthContext.Provider value={{ user, loading, isAuthenticated: !!user }}>
      {children}
    </AuthContext.Provider>
  );
}

export const useAuth = () => useContext(AuthContext);
