import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { CapabilitySettings } from "@/components/settings/capability-settings"
import { MonitoringSettings } from "@/components/settings/monitoring-settings"
import {
  api,
  type Capability,
  type MonitoringPreference,
  type SessionResponse,
} from "@/lib/api"
import {
  monitoringPreferenceKey,
  useMonitoringPreference,
} from "@/hooks/use-monitoring-preference"
import type { RefreshInterval } from "@/lib/monitoring"

export function SettingsPage() {
  const client = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const preference = useMonitoringPreference()
  const capabilities = useQuery({
    queryKey: ["capabilities"],
    queryFn: () => api<{ capabilities: Capability[] }>("/capabilities"),
  })
  const updatePreference = useMutation({
    mutationFn: (defaultInterval: RefreshInterval) =>
      api<MonitoringPreference>("/preferences/monitoring", {
        method: "PUT",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify({
          defaultInterval,
          expectedRevision: preference.data?.revision ?? 0,
        }),
      }),
    onSuccess: (value) => client.setQueryData(monitoringPreferenceKey, value),
  })

  return (
    <main className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <p className="text-sm text-muted-foreground">
        Server preferences and host integration capabilities.
      </p>
      <MonitoringSettings
        value={preference.data?.defaultInterval ?? "1m"}
        onChange={(value) => updatePreference.mutate(value)}
        error={preference.isError || updatePreference.isError}
        loadError={preference.isError}
        pending={preference.isPending}
      />
      <CapabilitySettings
        capabilities={capabilities.data?.capabilities ?? []}
        pending={capabilities.isPending}
        error={capabilities.isError}
      />
    </main>
  )
}
