import * as React from "react"

export type RefreshInterval = "off" | "1s" | "5s" | "15s" | "30s" | "1m" | "5m"

export const refreshIntervals: {
  value: RefreshInterval
  label: string
  milliseconds: number | false
}[] = [
  { value: "off", label: "Off", milliseconds: false },
  { value: "1s", label: "1 second", milliseconds: 1_000 },
  { value: "5s", label: "5 seconds", milliseconds: 5_000 },
  { value: "15s", label: "15 seconds", milliseconds: 15_000 },
  { value: "30s", label: "30 seconds", milliseconds: 30_000 },
  { value: "1m", label: "1 minute", milliseconds: 60_000 },
  { value: "5m", label: "5 minutes", milliseconds: 300_000 },
]

export function usePreference<T>(username: string, key: string, fallback: T) {
  const storageKey = `tako:${username}:${key}`
  const [value, setValue] = React.useState<T>(() => {
    try {
      const saved = localStorage.getItem(storageKey)
      return saved === null ? fallback : (JSON.parse(saved) as T)
    } catch {
      return fallback
    }
  })
  React.useEffect(() => {
    localStorage.setItem(storageKey, JSON.stringify(value))
  }, [storageKey, value])
  return [value, setValue] as const
}
