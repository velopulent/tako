import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import * as React from "react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"

const ntpItems = [
  { value: "enabled", label: "Enabled" },
  { value: "disabled", label: "Disabled" },
]

import { PowerControls } from "@/components/power-controls"
import {
  api,
  type HostConfiguration,
  type HostConfigurationPreview,
  type PowerStatus,
  type SessionResponse,
} from "@/lib/api"

type HostForm = Pick<HostConfiguration, "hostname" | "timezone" | "ntpEnabled">

const emptyForm: HostForm = { hostname: "", timezone: "UTC", ntpEnabled: true }

export function HostPage() {
  const client = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const admin = useQuery({
    queryKey: ["admin"],
    queryFn: () => api<{ administrative: boolean; until: string }>("/admin"),
    refetchInterval: 10_000,
  })
  const configuration = useQuery({
    queryKey: ["host-config"],
    queryFn: () => api<HostConfiguration>("/host/config"),
  })
  const power = useQuery({
    queryKey: ["host-power"],
    queryFn: () => api<PowerStatus>("/host/power"),
  })
  const [draft, setDraft] = React.useState<HostForm | null>(null)
  const form =
    draft ??
    (configuration.data
      ? {
          hostname: configuration.data.hostname,
          timezone: configuration.data.timezone,
          ntpEnabled: configuration.data.ntpEnabled,
        }
      : emptyForm)

  const payload = configuration.data
    ? { ...form, expectedFingerprint: configuration.data.fingerprint }
    : undefined
  const preview = useMutation({
    mutationFn: () =>
      api<HostConfigurationPreview>("/host/config/preview", {
        method: "POST",
        body: JSON.stringify(payload),
      }),
  })
  const update = useMutation({
    mutationFn: () =>
      api<HostConfiguration>("/host/config", {
        method: "PUT",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(payload),
      }),
    onSuccess: (value) => {
      client.setQueryData(["host-config"], value)
      setDraft(null)
      preview.reset()
    },
  })

  if (configuration.isPending)
    return (
      <main className="p-6">
        <Skeleton className="h-96" />
      </main>
    )
  if (configuration.isError || !configuration.data)
    return (
      <main className="p-6">
        <Alert variant="destructive">
          <AlertTitle>Host configuration unavailable</AlertTitle>
          <AlertDescription>
            Hostname and time configuration could not be read.
          </AlertDescription>
        </Alert>
      </main>
    )

  return (
    <main className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <p className="text-sm text-muted-foreground">
        Preview and apply hostname, timezone, and NTP changes through the
        host&apos;s system service. Every write is fingerprint-checked and
        verified.
      </p>
      {!admin.data?.administrative && (
        <Alert>
          <AlertTitle>Administrative access required</AlertTitle>
          <AlertDescription>
            Gain Administrative access from the header before applying changes.
          </AlertDescription>
        </Alert>
      )}
      {(preview.isError || update.isError) && (
        <Alert variant="destructive">
          <AlertTitle>Host configuration action failed</AlertTitle>
          <AlertDescription>
            {(preview.error || update.error)?.message}
          </AlertDescription>
        </Alert>
      )}
      {update.isSuccess && (
        <Alert>
          <AlertTitle>Host configuration updated</AlertTitle>
          <AlertDescription>
            Changes were applied and verified.
          </AlertDescription>
        </Alert>
      )}
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <CardTitle>Host identity and time</CardTitle>
              <CardDescription>
                Current fingerprint{" "}
                {configuration.data.fingerprint.slice(0, 12)}…
              </CardDescription>
            </div>
            <Badge
              variant={admin.data?.administrative ? "secondary" : "outline"}
            >
              {admin.data?.administrative ? "Administrative" : "Read only"}
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="space-y-6">
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="hostname">Hostname</FieldLabel>
              <Input
                id="hostname"
                maxLength={253}
                value={form.hostname}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...(current ?? form),
                    hostname: event.target.value,
                  }))
                }
              />
              <FieldDescription>
                A DNS-safe host name; changes affect the local system identity.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="timezone">Timezone</FieldLabel>
              <Input
                id="timezone"
                maxLength={128}
                value={form.timezone}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...(current ?? form),
                    timezone: event.target.value,
                  }))
                }
              />
              <FieldDescription>
                Use an IANA timezone such as UTC or Asia/Kolkata.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="ntp">
                Network time synchronization
              </FieldLabel>
              <Select
                items={ntpItems}
                value={form.ntpEnabled ? "enabled" : "disabled"}
                onValueChange={(value) =>
                  setDraft((current) => ({
                    ...(current ?? form),
                    ntpEnabled: value === "enabled",
                  }))
                }
              >
                <SelectTrigger
                  id="ntp"
                  aria-label="Network time synchronization"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {ntpItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>
                The system&apos;s timedate service controls this setting.
              </FieldDescription>
            </Field>
          </FieldGroup>
          {preview.data && (
            <Alert variant={preview.data.stale ? "destructive" : "default"}>
              <AlertTitle>
                {preview.data.stale ? "Refresh required" : "Preview"}
              </AlertTitle>
              <AlertDescription>
                {preview.data.stale
                  ? "Another browser changed this host. Load the current state before applying."
                  : preview.data.changes.length
                    ? `Changes: ${preview.data.changes.join(", ")}.`
                    : "No changes detected."}
              </AlertDescription>
            </Alert>
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={preview.isPending}
              onClick={() => preview.mutate()}
            >
              {preview.isPending ? "Preparing…" : "Preview changes"}
            </Button>
            <Button
              disabled={
                !admin.data?.administrative || update.isPending || !payload
              }
              onClick={() => update.mutate()}
            >
              {update.isPending ? "Applying…" : "Apply and verify"}
            </Button>
          </div>
        </CardContent>
      </Card>
      {power.isPending && <Skeleton className="h-72" />}
      {power.isError && (
        <Alert variant="destructive">
          <AlertTitle>Power controls unavailable</AlertTitle>
          <AlertDescription>
            The host power-management adapter could not be read.
          </AlertDescription>
        </Alert>
      )}
      {power.data && (
        <PowerControls
          status={power.data}
          csrfToken={session.data?.csrfToken ?? ""}
          administrative={admin.data?.administrative ?? false}
        />
      )}
    </main>
  )
}

export const Route = createFileRoute("/host")({
  component: HostPage,
})
