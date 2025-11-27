import { Routes, Route } from 'react-router-dom'
import { useAuth } from './lib/auth'
import { useTheme } from './lib/theme'
import { LoginPage } from './components/LoginPage'
import { DashboardPage } from './pages/DashboardPage'
import { InstanceDetailPage } from './pages/InstanceDetailPage'
import { Toaster } from './components/ui/toaster'
import { Sun, Moon } from 'lucide-react'

function App() {
  const { isAuthenticated, logout } = useAuth();
  const { theme, toggleTheme } = useTheme();

  if (!isAuthenticated) {
    return <LoginPage />;
  }

  return (
    <div className="min-h-screen bg-white dark:bg-gray-900">
      {/* Global Topbar */}
      <header className="sticky top-0 z-50 bg-gray-100 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 shadow-sm">
        <div className="px-4 mx-auto max-w-7xl sm:px-6 lg:px-8">
          <div className="flex justify-between items-center h-16">
            <h1 className="mb-0 text-xl font-semibold text-gray-900 dark:text-white">
              Gowa Manager
            </h1>
            <div className="flex items-center gap-2">
              <button
                onClick={toggleTheme}
                className="p-2 text-gray-600 dark:text-gray-300 bg-gray-200 dark:bg-gray-700 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm transition-colors hover:bg-gray-300 dark:hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500"
                aria-label="Toggle theme"
              >
                {theme === 'dark' ? <Sun className="w-5 h-5" /> : <Moon className="w-5 h-5" />}
              </button>
              <button
                onClick={logout}
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
      </Routes>

      {/* Toast notifications */}
      <Toaster />
    </div>
  )
}

export default App
