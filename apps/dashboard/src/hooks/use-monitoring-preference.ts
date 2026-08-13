import { useQuery } from "@tanstack/react-query"

import { api, type MonitoringPreference } from "@/lib/api"

export const monitoringPreferenceKey = ["preferences", "monitoring"] as const

export function useMonitoringPreference() {
  return useQuery<MonitoringPreference>({
    queryKey: monitoringPreferenceKey,
    queryFn: () => api<MonitoringPreference>("/preferences/monitoring"),
    staleTime: 5 * 60_000,
  })
}
