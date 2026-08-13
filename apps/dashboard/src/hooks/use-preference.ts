import * as React from "react"

export function usePreference<T>(username: string, key: string, fallback: T) {
  const storageKey = `tako:${username}:${key}`
  const [preference, setPreference] = React.useState<
    { stored: true; value: T } | { stored: false }
  >(() => {
    try {
      const saved = localStorage.getItem(storageKey)
      return saved === null
        ? { stored: false }
        : { stored: true, value: JSON.parse(saved) as T }
    } catch {
      return { stored: false }
    }
  })
  const saveValue = React.useCallback(
    (next: T) => {
      setPreference({ stored: true, value: next })
      localStorage.setItem(storageKey, JSON.stringify(next))
    },
    [storageKey]
  )
  return [preference.stored ? preference.value : fallback, saveValue] as const
}
