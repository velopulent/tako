import { useQuery, useQueryClient } from "@tanstack/react-query"

import type { RefreshInterval } from "@/lib/monitoring"

export const monitoringPreferenceKey = ["preferences", "monitoring"] as const

const storageKey = "tako-monitoring-default:v1"
const fallback: RefreshInterval = "1m"

function readStored(): RefreshInterval {
  try {
    const raw = localStorage.getItem(storageKey)
    if (
      raw === "off" ||
      raw === "1s" ||
      raw === "5s" ||
      raw === "15s" ||
      raw === "30s" ||
      raw === "1m" ||
      raw === "5m"
    ) {
      return raw
    }
  } catch {
    // ignore
  }
  return fallback
}

export function useMonitoringPreference() {
  return useQuery({
    queryKey: monitoringPreferenceKey,
    queryFn: async () => ({ defaultInterval: readStored() }),
    staleTime: Infinity,
  })
}

export function useSetMonitoringPreference() {
  const client = useQueryClient()
  return async (value: RefreshInterval) => {
    try {
      localStorage.setItem(storageKey, value)
    } catch {
      // ignore
    }
    client.setQueryData(monitoringPreferenceKey, { defaultInterval: value })
  }
}
