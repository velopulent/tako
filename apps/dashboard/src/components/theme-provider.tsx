import * as React from "react"

export type Theme = "light" | "dark" | "system"

type ThemeProviderState = {
  theme: Theme
  resolvedTheme: "light" | "dark"
  setTheme: (theme: Theme) => void
}

const storageKey = "tako-theme"
const colorSchemeQuery = "(prefers-color-scheme: dark)"

const ThemeProviderContext = React.createContext<
  ThemeProviderState | undefined
>(undefined)

function readTheme(): Theme {
  try {
    const stored = localStorage.getItem(storageKey)
    if (stored === "light" || stored === "dark" || stored === "system")
      return stored
  } catch {
    // Storage may be unavailable; fall back to the system preference.
  }
  return "system"
}

function systemTheme(): "light" | "dark" {
  return window.matchMedia(colorSchemeQuery).matches ? "dark" : "light"
}

// applyTheme switches the class without animating every color transition.
function applyTheme(resolved: "light" | "dark") {
  const root = document.documentElement
  const style = document.createElement("style")
  style.textContent = "*,*::before,*::after{transition:none!important}"
  document.head.appendChild(style)
  root.classList.toggle("dark", resolved === "dark")
  root.style.colorScheme = resolved
  window.getComputedStyle(document.body)
  requestAnimationFrame(() => style.remove())
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = React.useState<Theme>(readTheme)
  const [system, setSystem] = React.useState(systemTheme)
  const resolvedTheme = theme === "system" ? system : theme

  React.useEffect(() => {
    const media = window.matchMedia(colorSchemeQuery)
    const update = () => setSystem(media.matches ? "dark" : "light")
    media.addEventListener("change", update)
    return () => media.removeEventListener("change", update)
  }, [])

  React.useLayoutEffect(() => applyTheme(resolvedTheme), [resolvedTheme])

  React.useEffect(() => {
    const sync = (event: StorageEvent) => {
      if (event.key === storageKey) setThemeState(readTheme())
    }
    window.addEventListener("storage", sync)
    return () => window.removeEventListener("storage", sync)
  }, [])

  const value = React.useMemo<ThemeProviderState>(
    () => ({
      theme,
      resolvedTheme,
      setTheme: (next) => {
        try {
          localStorage.setItem(storageKey, next)
        } catch {
          // The choice still applies for this page.
        }
        setThemeState(next)
      },
    }),
    [theme, resolvedTheme]
  )

  return (
    <ThemeProviderContext.Provider value={value}>
      {children}
    </ThemeProviderContext.Provider>
  )
}

export function useTheme() {
  const context = React.useContext(ThemeProviderContext)
  if (context === undefined) {
    throw new Error("useTheme must be used within a ThemeProvider")
  }
  return context
}
