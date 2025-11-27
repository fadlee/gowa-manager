import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { AuthProvider } from './lib/auth'
import './index.css'

// Set up default authorization header for all requests
const setupAuthInterceptor = () => {
  const originalFetch = window.fetch;
  (window as any).fetch = function(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const headers = new Headers(init?.headers);

    // Add stored credentials if available
    const storedAuth = localStorage.getItem('gowa_auth');
    if (storedAuth) {
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
    <BrowserRouter>
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <App />
        </AuthProvider>
      </QueryClientProvider>
    </BrowserRouter>
  </React.StrictMode>,
)
