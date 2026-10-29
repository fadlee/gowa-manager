import React, { createContext, useContext, useEffect, ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from './api';
import type { User } from '../types';

interface AuthContextType {
  isLoading: boolean;
  needsSetup: boolean;
  user: User | null;
  isAuthenticated: boolean;
  login: (username: string, password: string) => Promise<void>;
  setup: (username: string, password: string, setupToken: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

const AUTH_STATUS_KEY = ['authStatus'];

export const AuthProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const queryClient = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: AUTH_STATUS_KEY,
    queryFn: () => apiClient.getAuthStatus(),
    staleTime: Infinity,
  });

  useEffect(() => {
    // Credentials are no longer kept in the browser; drop the legacy entry.
    localStorage.removeItem('gowa_auth');
    apiClient.onUnauthorized = () => {
      queryClient.invalidateQueries({ queryKey: AUTH_STATUS_KEY });
    };
    return () => {
      apiClient.onUnauthorized = null;
    };
  }, [queryClient]);

  const signedIn = (user: User) => {
    queryClient.setQueryData(AUTH_STATUS_KEY, { needsSetup: false, user });
  };

  const login = async (username: string, password: string) => {
    const { user } = await apiClient.login(username, password);
    signedIn(user);
  };

  const setup = async (username: string, password: string, setupToken: string) => {
    const { user } = await apiClient.setup(username, password, setupToken);
    signedIn(user);
  };

  const logout = async () => {
    try {
      await apiClient.logout();
    } finally {
      queryClient.setQueryData(AUTH_STATUS_KEY, { needsSetup: false, user: null });
      // Drop the signed-out user's cached data, but keep the auth status
      // query that App is subscribed to.
      queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== AUTH_STATUS_KEY[0] });
    }
  };

  const user = data?.user ?? null;

  return (
    <AuthContext.Provider
      value={{
        isLoading,
        needsSetup: data?.needsSetup ?? false,
        user,
        isAuthenticated: user !== null,
        login,
        setup,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = (): AuthContextType => {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};
