import { useQuery } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"

import { CapabilitySettings } from "@/components/settings/capability-settings"
import { MonitoringSettings } from "@/components/settings/monitoring-settings"
import {
  useMonitoringPreference,
  useSetMonitoringPreference,
} from "@/hooks/use-monitoring-preference"
import { api, type Capability } from "@/lib/api"

export function SettingsPage() {
  const preference = useMonitoringPreference()
  const setPreference = useSetMonitoringPreference()
  const capabilities = useQuery({
    queryKey: ["capabilities"],
    queryFn: () => api<{ capabilities: Capability[] }>("/capabilities"),
  })

  return (
    <main className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <p className="text-sm text-muted-foreground">
        Browser preferences and host integration capabilities.
      </p>
      <MonitoringSettings
        value={preference.data?.defaultInterval ?? "1m"}
        onChange={(value) => void setPreference(value)}
        error={preference.isError}
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

export const Route = createFileRoute("/settings")({
  component: SettingsPage,
})
