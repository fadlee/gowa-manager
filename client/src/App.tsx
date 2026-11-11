import { Routes, Route, Link, Navigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useAuth } from './lib/auth'
import { apiClient } from './lib/api'
import { LoginPage, SetupPage } from './components/LoginPage'
import { WorkspacePage } from './pages/WorkspacePage'
import { AccountPage } from './pages/AccountPage'
import { UsersPage } from './pages/UsersPage'
import { Toaster } from './components/ui/toaster'
import { BellRing, MessagesSquare, Search } from 'lucide-react'
import { useEffect, useState } from 'react'
import { CommandPalette } from './components/CommandPalette'
import { UserMenu } from './components/UserMenu'
import { hasNewerVersion } from './lib/version'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './components/ui/tooltip'

function App() {
  const { isLoading: authLoading, needsSetup, user, isAuthenticated, logout } = useAuth();
  const [paletteOpen, setPaletteOpen] = useState(false);
  const shortcutLabel = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘K' : 'Ctrl K';

  // Ctrl/⌘ K opens the command palette from anywhere.
  useEffect(() => {
    if (!isAuthenticated) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        setPaletteOpen(open => !open);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [isAuthenticated]);

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
    return <div className="min-h-screen bg-white dark:bg-gray-950" aria-busy="true" />;
  }

  if (needsSetup) {
    return <SetupPage />;
  }

  if (!isAuthenticated) {
    return <LoginPage />;
  }

  const isSuperadmin = user?.role === 'superadmin';

  return (
    <div className="min-h-screen bg-white dark:bg-gray-950">
      <header className="sticky top-0 z-40 h-14 border-b border-gray-200 bg-white/90 backdrop-blur dark:border-gray-800 dark:bg-gray-950/90">
        <div className="flex h-full items-center gap-3 px-3 sm:px-4">
          <Link to="/" className="flex min-w-0 items-center gap-2.5">
            <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-indigo-600 text-white shadow-sm">
              <MessagesSquare className="h-[18px] w-[18px]" />
            </span>
            <span className="truncate text-base font-semibold text-gray-900 dark:text-white">Gowa Manager</span>
            {systemStatus && (
              <span className="hidden text-xs text-gray-500 dark:text-gray-400 sm:inline">v{systemStatus.managerVersion}</span>
            )}
          </Link>
          {managerUpdateAvailable && (
            <TooltipProvider delayDuration={150}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span
                    role="img"
                    tabIndex={0}
                    aria-label={`New Gowa Manager version ${latestManagerVersion} available`}
                    className="relative inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-amber-500 transition-colors hover:bg-amber-50 hover:text-amber-600 focus:outline-none focus:ring-2 focus:ring-amber-400 dark:hover:bg-amber-950/40"
                  >
                    <BellRing className="h-4 w-4" />
                    <span className="absolute right-1.5 top-1.5 h-2 w-2 rounded-full bg-amber-500 ring-2 ring-white dark:ring-gray-950" />
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
          <span className="flex-1" />
          <button
            type="button"
            onClick={() => setPaletteOpen(true)}
            className="hidden h-9 w-72 items-center gap-2 whitespace-nowrap rounded-md border border-gray-300 bg-gray-50 px-3 text-sm text-gray-500 transition-colors hover:border-indigo-400 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-400 md:inline-flex"
          >
            <Search className="h-4 w-4" />
            <span className="flex-1 truncate text-left">Jump to instance or action…</span>
            <kbd className="rounded border border-gray-300 px-1.5 text-[10px] dark:border-gray-700">{shortcutLabel}</kbd>
          </button>
          <button
            type="button"
            onClick={() => setPaletteOpen(true)}
            aria-label="Search instances and actions"
            className="rounded-md p-2 text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800 md:hidden"
          >
            <Search className="h-5 w-5" />
          </button>
          <UserMenu user={user} onLogout={() => { void logout(); }} />
        </div>
      </header>

      <Routes>
        <Route path="/" element={<WorkspacePage />} />
        <Route path="/instances/:id" element={<WorkspacePage />} />
        <Route path="/account" element={<AccountPage />} />
        <Route path="/users" element={isSuperadmin ? <UsersPage /> : <Navigate to="/" replace />} />
      </Routes>

      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} isSuperadmin={isSuperadmin} />
      <Toaster />
    </div>
  )
}

export default App
