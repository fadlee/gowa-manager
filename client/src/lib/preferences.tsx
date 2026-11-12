import { createContext, useContext, useEffect, useState } from 'react'

export type Density = 'compact' | 'comfortable'

interface PreferencesContextType {
  density: Density
  setDensity: (density: Density) => void
}

const PreferencesContext = createContext<PreferencesContextType>({
  density: 'compact',
  setDensity: () => {},
})

function readDensity(): Density {
  try {
    return localStorage.getItem('density') === 'comfortable' ? 'comfortable' : 'compact'
  } catch {
    return 'compact'
  }
}

export function PreferencesProvider({ children }: { children: React.ReactNode }) {
  const [density, setDensity] = useState<Density>(readDensity)

  useEffect(() => {
    try {
      localStorage.setItem('density', density)
    } catch {
      // Storage may be unavailable (private mode); the choice still applies for this session.
    }
  }, [density])

  return (
    <PreferencesContext.Provider value={{ density, setDensity }}>
      {children}
    </PreferencesContext.Provider>
  )
}

export const usePreferences = () => useContext(PreferencesContext)
