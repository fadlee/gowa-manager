import { Routes, Route } from 'react-router-dom'
import { useAuth } from './lib/auth'
import { LoginPage } from './components/LoginPage'
import { DashboardPage } from './pages/DashboardPage'
import { InstanceDetailPage } from './pages/InstanceDetailPage'

function App() {
  const { isAuthenticated, logout } = useAuth();

  if (!isAuthenticated) {
    return <LoginPage />;
  }

  return (
    <div className="min-h-screen bg-gray-900">
      {/* Global Topbar */}
      <header className="bg-gray-800 border-b border-gray-700 shadow-sm sticky top-0 z-50">
        <div className="px-4 mx-auto max-w-7xl sm:px-6 lg:px-8">
          <div className="flex justify-between items-center h-16">
            <h1 className="text-xl font-semibold text-white">
              Gowa Manager
            </h1>
            <button
              onClick={logout}
              className="px-4 py-2 text-sm font-medium text-gray-300 bg-gray-700 border border-gray-600 rounded-md shadow-sm hover:bg-gray-600 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500 transition-colors"
            >
              Logout
            </button>
          </div>
        </div>
      </header>

      {/* Page Content */}
      <Routes>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/instances/:id" element={<InstanceDetailPage />} />
      </Routes>
    </div>
  )
}

export default App
