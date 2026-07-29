import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { AuthProvider } from './lib/auth'
import { ThemeProvider } from './lib/theme'
import './index.css'

// Set up default authorization header for all requests
const setupAuthInterceptor = () => {
  const originalFetch = window.fetch;
  window.fetch = function(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const headers = new Headers(init?.headers);
    const requestUrl = new URL(input instanceof Request ? input.url : input.toString(), window.location.origin);

    // Add stored credentials only for manager API calls.
    const storedAuth = localStorage.getItem('gowa_auth');
    if (storedAuth && requestUrl.origin === window.location.origin && requestUrl.pathname.startsWith('/api/')) {
      headers.set('Authorization', `Basic ${storedAuth}`);
    }

    return originalFetch(input, {
      ...init,
      headers
    });
  };
};

setupAuthInterceptor();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30000,
      refetchOnWindowFocus: false,
    },
  },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ThemeProvider>
      <BrowserRouter>
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <App />
          </AuthProvider>
        </QueryClientProvider>
      </BrowserRouter>
    </ThemeProvider>
  </React.StrictMode>,
)
