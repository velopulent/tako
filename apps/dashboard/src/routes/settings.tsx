import { useQuery } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import { MonitorIcon, MoonIcon, SunIcon } from "lucide-react"

import { RefreshSelect } from "@/components/refresh-select"
import { StatusBadge } from "@/components/status-badge"
import { useTheme } from "@/components/theme-provider"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  useMonitoringPreference,
  useSetMonitoringPreference,
} from "@/hooks/use-monitoring-preference"
import { api, type Capability } from "@/lib/api"
import { Page, QueryError } from "@/lib/page"

const capabilityNames: Record<string, string> = {
  dashboard: "Dashboard",
  metrics: "Metrics",
  processes: "Processes",
  users: "Accounts",
  files: "Files",
  terminal: "Terminal",
  services: "Services and timers",
  logs: "Logs",
  storage: "Storage",
  updates: "Software updates",
  network: "Network",
  firewall: "Firewall",
  selinux: "SELinux",
  apparmor: "AppArmor",
}

const stateTone = {
  ready: "success",
  degraded: "warning",
  conflicted: "danger",
  unavailable: "muted",
} as const
const stateLabel = {
  ready: "Ready",
  degraded: "Limited",
  conflicted: "Conflict",
  unavailable: "Unavailable",
} as const

function SettingsPage() {
  const { theme, setTheme } = useTheme()
  const preference = useMonitoringPreference()
  const setPreference = useSetMonitoringPreference()
  const capabilities = useQuery({
    queryKey: ["capabilities"],
    queryFn: () => api<{ capabilities: Capability[] }>("/capabilities"),
  })
  const items = [...(capabilities.data?.capabilities ?? [])]
    .filter((item) => item.id !== "dashboard" && item.id !== "platform")
    .sort(
      (left, right) =>
        Number(left.state !== "ready") - Number(right.state !== "ready")
    )
  return (
    <Page description="Preferences for this browser, and what this host supports.">
      <div className="grid gap-6 @4xl/main:grid-cols-2">
        <Card className="h-fit">
          <CardHeader>
            <CardTitle>Preferences</CardTitle>
            <CardDescription>Saved in this browser only.</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldLabel>Appearance</FieldLabel>
                  <FieldDescription>
                    System follows your device setting.
                  </FieldDescription>
                </FieldContent>
                <ToggleGroup
                  value={[theme]}
                  onValueChange={(value) =>
                    value[0] && setTheme(value[0] as typeof theme)
                  }
                  variant="outline"
                  size="sm"
                >
                  <ToggleGroupItem value="light" aria-label="Light">
                    <SunIcon />
                  </ToggleGroupItem>
                  <ToggleGroupItem value="dark" aria-label="Dark">
                    <MoonIcon />
                  </ToggleGroupItem>
                  <ToggleGroupItem value="system" aria-label="System">
                    <MonitorIcon />
                  </ToggleGroupItem>
                </ToggleGroup>
              </Field>
              <Separator />
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldLabel>Default refresh</FieldLabel>
                  <FieldDescription>
                    How often live pages update unless you change it on the
                    page.
                  </FieldDescription>
                </FieldContent>
                {preference.isPending ? (
                  <Skeleton className="h-8 w-28" />
                ) : (
                  <RefreshSelect
                    value={preference.data?.defaultInterval ?? "5s"}
                    onChange={(value) => void setPreference(value)}
                  />
                )}
              </Field>
            </FieldGroup>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Host support</CardTitle>
            <CardDescription>
              Features Tako detected on this machine.
            </CardDescription>
          </CardHeader>
          <CardContent>
            {capabilities.isPending ? (
              <Skeleton className="h-64" />
            ) : capabilities.isError ? (
              <QueryError
                error={capabilities.error}
                retry={() => void capabilities.refetch()}
              />
            ) : (
              <ul className="flex flex-col divide-y">
                {items.map((item) => (
                  <li
                    key={item.id}
                    className="flex items-start gap-3 py-2.5 first:pt-0 last:pb-0"
                  >
                    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <span className="text-sm font-medium">
                        {capabilityNames[item.id] ?? item.id}
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {[item.backend, item.version].filter(Boolean).join(" ")}
                        {item.state === "ready" &&
                        !item.mutable &&
                        item.readable
                          ? " · view only"
                          : ""}
                      </span>
                      {item.state !== "ready" &&
                        (item.reason || item.setupGuidance) && (
                          <span className="text-xs text-muted-foreground">
                            {[item.reason, item.setupGuidance]
                              .filter(Boolean)
                              .join(". ")}
                          </span>
                        )}
                    </span>
                    <StatusBadge tone={stateTone[item.state]}>
                      {stateLabel[item.state]}
                    </StatusBadge>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>
    </Page>
  )
}

export const Route = createFileRoute("/settings")({ component: SettingsPage })
