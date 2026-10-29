import { Routes, Route, Link, Navigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useAuth } from './lib/auth'
import { useTheme } from './lib/theme'
import { apiClient } from './lib/api'
import { LoginPage, SetupPage } from './components/LoginPage'
import { DashboardPage } from './pages/DashboardPage'
import { InstanceDetailPage } from './pages/InstanceDetailPage'
import { AccountPage } from './pages/AccountPage'
import { UsersPage } from './pages/UsersPage'
import { Toaster } from './components/ui/toaster'
import { BellRing, Sun, Moon, UserCircle, Users } from 'lucide-react'
import { hasNewerVersion } from './lib/version'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './components/ui/tooltip'

function App() {
  const { isLoading: authLoading, needsSetup, user, isAuthenticated, logout } = useAuth();
  const { theme, toggleTheme } = useTheme();

  const { data: systemStatus } = useQuery({
    queryKey: ['systemStatus'],
    queryFn: () => apiClient.getSystemStatus(),
    enabled: isAuthenticated,
    staleTime: 5 * 60 * 1000,
  });

  const { data: latestManagerVersion } = useQuery({
    queryKey: ['managerVersion', 'latest'],
    queryFn: () => apiClient.getLatestManagerVersion(),
    enabled: isAuthenticated,
    staleTime: 30 * 60 * 1000,
  });

  const managerUpdateAvailable = hasNewerVersion(systemStatus?.managerVersion, latestManagerVersion);

  if (authLoading) {
    return <div className="min-h-screen bg-gray-100 dark:bg-gray-900" aria-busy="true" />;
  }

  if (needsSetup) {
    return <SetupPage />;
  }

  if (!isAuthenticated) {
    return <LoginPage />;
  }

  const isSuperadmin = user?.role === 'superadmin';
  const navButtonClass = "inline-flex items-center gap-1.5 px-3 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-200 dark:bg-gray-700 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm transition-colors hover:bg-gray-300 dark:hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500";

  return (
    <div className="min-h-screen bg-white dark:bg-gray-900">
      {/* Global Topbar */}
      <header className="sticky top-0 z-50 bg-gray-100 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 shadow-sm">
        <div className="px-4 mx-auto max-w-7xl sm:px-6 lg:px-8">
          <div className="flex justify-between items-center h-16">
            <div className="flex items-center gap-2 min-w-0">
              <h1 className="mb-0 text-xl font-semibold text-gray-900 truncate dark:text-white">
                <Link to="/">Gowa Manager</Link>
                {systemStatus && (
                  <span className="ml-2 text-sm font-normal text-gray-500 dark:text-gray-400">
                    v{systemStatus.managerVersion}
                  </span>
                )}
              </h1>
              {managerUpdateAvailable && (
                <TooltipProvider delayDuration={150}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span
                        role="img"
                        tabIndex={0}
                        aria-label={`New Gowa Manager version ${latestManagerVersion} available`}
                        className="relative inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-amber-500 transition-colors hover:bg-amber-50 hover:text-amber-600 focus:outline-none focus:ring-2 focus:ring-amber-400 focus:ring-offset-2 dark:hover:bg-amber-950/40 dark:hover:text-amber-300"
                      >
                        <BellRing className="h-4 w-4" />
                        <span className="absolute right-1.5 top-1.5 h-2 w-2 rounded-full bg-amber-500 ring-2 ring-gray-100 dark:ring-gray-800" />
                      </span>
                    </TooltipTrigger>
                    <TooltipContent className="max-w-64 text-xs leading-relaxed">
                      <p className="font-medium">New Gowa Manager version available</p>
                      <p className="mt-1 text-gray-600 dark:text-gray-300">
                        Current version is v{systemStatus?.managerVersion}. Latest release is {latestManagerVersion}.
                      </p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              )}
            </div>
            <div className="flex items-center gap-2">
              {isSuperadmin && (
                <Link to="/users" className={navButtonClass} aria-label="Users">
                  <Users className="w-4 h-4" />
                  <span className="hidden sm:inline">Users</span>
                </Link>
              )}
              <Link to="/account" className={navButtonClass} aria-label="My account">
                <UserCircle className="w-4 h-4" />
                <span className="hidden sm:inline max-w-32 truncate">{user?.username}</span>
              </Link>
              <button
                onClick={toggleTheme}
                className="p-2 text-gray-600 dark:text-gray-300 bg-gray-200 dark:bg-gray-700 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm transition-colors hover:bg-gray-300 dark:hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500"
                aria-label="Toggle theme"
              >
                {theme === 'dark' ? <Sun className="w-5 h-5" /> : <Moon className="w-5 h-5" />}
              </button>
              <button
                onClick={() => { void logout(); }}
                className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-200 dark:bg-gray-700 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm transition-colors hover:bg-gray-300 dark:hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500"
              >
                Logout
              </button>
            </div>
          </div>
        </div>
      </header>

      {/* Page Content */}
      <Routes>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/instances/:id" element={<InstanceDetailPage />} />
        <Route path="/account" element={<AccountPage />} />
        <Route path="/users" element={isSuperadmin ? <UsersPage /> : <Navigate to="/" replace />} />
      </Routes>

      {/* Toast notifications */}
      <Toaster />
    </div>
  )
}

export default App
