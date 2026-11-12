import { createContext, useContext, useState } from 'react'

export type Density = 'compact' | 'comfortable'

const DEFAULT_DENSITY: Density = 'comfortable'

interface PreferencesContextType {
  density: Density
  setDensity: (density: Density) => void
}

const PreferencesContext = createContext<PreferencesContextType>({
  density: DEFAULT_DENSITY,
  setDensity: () => {},
})

function readDensity(): Density {
  try {
    const stored = localStorage.getItem('density')
    return stored === 'compact' || stored === 'comfortable' ? stored : DEFAULT_DENSITY
  } catch {
    return DEFAULT_DENSITY
  }
}

export function PreferencesProvider({ children }: { children: React.ReactNode }) {
  const [density, setDensityState] = useState<Density>(readDensity)

  // Only an explicit choice is stored, so users who never picked one follow the default.
  const setDensity = (next: Density) => {
    setDensityState(next)
    try {
      localStorage.setItem('density', next)
    } catch {
      // Storage may be unavailable (private mode); the choice still applies for this session.
    }
  }

  return (
    <PreferencesContext.Provider value={{ density, setDensity }}>
      {children}
    </PreferencesContext.Provider>
  )
}

export const usePreferences = () => useContext(PreferencesContext)
