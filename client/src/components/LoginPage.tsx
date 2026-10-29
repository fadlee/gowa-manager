import React, { useState } from 'react';
import { useAuth } from '../lib/auth';
import { useTheme } from '../lib/theme';
import { Sun, Moon } from 'lucide-react';

const inputClassName = 'appearance-none block w-full px-3 py-2 bg-gray-50 dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md shadow-sm placeholder-gray-400 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500 transition-colors';

interface FieldProps {
  id: string;
  label: string;
  type?: string;
  value: string;
  onChange: (value: string) => void;
  autoComplete?: string;
  hint?: string;
}

const Field: React.FC<FieldProps> = ({ id, label, type = 'text', value, onChange, autoComplete, hint }) => (
  <div>
    <label htmlFor={id} className="block text-sm font-medium text-gray-700 dark:text-gray-300">
      {label}
    </label>
    <div className="mt-1">
      <input
        id={id}
        name={id}
        type={type}
        required
        autoComplete={autoComplete}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={inputClassName}
      />
    </div>
    {hint && <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">{hint}</p>}
  </div>
);

interface AuthShellProps {
  title: string;
  subtitle?: string;
  error: string;
  loading: boolean;
  submitLabel: string;
  loadingLabel: string;
  onSubmit: (e: React.FormEvent) => void;
  children: React.ReactNode;
}

const AuthShell: React.FC<AuthShellProps> = ({ title, subtitle, error, loading, submitLabel, loadingLabel, onSubmit, children }) => {
  const { theme, toggleTheme } = useTheme();

  return (
    <div className="min-h-screen bg-gray-100 dark:bg-gray-900 flex flex-col justify-center py-12 px-4 sm:px-6 lg:px-8">
      {/* Theme toggle */}
      <div className="absolute top-4 right-4">
        <button
          onClick={toggleTheme}
          className="p-2 text-gray-600 dark:text-gray-300 bg-white dark:bg-gray-700 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm transition-colors hover:bg-gray-100 dark:hover:bg-gray-600"
          aria-label="Toggle theme"
        >
          {theme === 'dark' ? <Sun className="w-5 h-5" /> : <Moon className="w-5 h-5" />}
        </button>
      </div>

      <div className="sm:mx-auto sm:w-full sm:max-w-md">
        <h2 className="mt-6 text-center text-3xl font-extrabold text-gray-900 dark:text-white">{title}</h2>
        {subtitle && <p className="mt-2 text-center text-sm text-gray-600 dark:text-gray-400">{subtitle}</p>}
      </div>

      <div className="mt-8 sm:mx-auto sm:w-full sm:max-w-md">
        <div className="bg-white dark:bg-gray-800 py-8 px-4 shadow-lg border border-gray-200 dark:border-gray-700 sm:rounded-lg sm:px-10">
          <form className="space-y-6" onSubmit={onSubmit}>
            {children}

            {error && (
              <div role="alert" className="text-red-600 dark:text-red-400 text-sm bg-red-100 dark:bg-red-900/20 border border-red-300 dark:border-red-800 rounded-md px-3 py-2">{error}</div>
            )}

            <div>
              <button
                type="submit"
                disabled={loading}
                className="w-full flex justify-center py-2.5 px-4 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-indigo-600 hover:bg-indigo-500 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-offset-white dark:focus:ring-offset-gray-800 focus:ring-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
              >
                {loading ? loadingLabel : submitLabel}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  );
};

export const LoginPage: React.FC = () => {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const { login } = useAuth();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError('');
    try {
      await login(username, password);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  return (
    <AuthShell title="Sign in to GOWA Manager" error={error} loading={loading} submitLabel="Sign in" loadingLabel="Signing in..." onSubmit={handleSubmit}>
      <Field id="username" label="Username" value={username} onChange={setUsername} autoComplete="username" />
      <Field id="password" label="Password" type="password" value={password} onChange={setPassword} autoComplete="current-password" />
    </AuthShell>
  );
};

export const SetupPage: React.FC = () => {
  const [setupToken, setSetupToken] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const { setup } = useAuth();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (password !== confirm) {
      setError('Passwords do not match');
      return;
    }
    setLoading(true);
    try {
      await setup(username, password, setupToken);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  return (
    <AuthShell
      title="Create the first account"
      subtitle="No users exist yet. This account becomes the superadmin."
      error={error}
      loading={loading}
      submitLabel="Create account"
      loadingLabel="Creating account..."
      onSubmit={handleSubmit}
    >
      <Field
        id="setupToken"
        label="Setup token"
        value={setupToken}
        onChange={setSetupToken}
        autoComplete="off"
        hint="Printed in the GOWA Manager startup output (server logs)."
      />
      <Field id="username" label="Username" value={username} onChange={setUsername} autoComplete="username" />
      <Field id="password" label="Password" type="password" value={password} onChange={setPassword} autoComplete="new-password" hint="At least 8 characters." />
      <Field id="confirmPassword" label="Confirm password" type="password" value={confirm} onChange={setConfirm} autoComplete="new-password" />
    </AuthShell>
  );
};
