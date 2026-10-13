import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import { PencilIcon, PowerIcon, RotateCcwIcon } from "lucide-react"
import * as React from "react"

import { AdminRequired, useAdminAccess } from "@/components/admin-access"
import { StatusBadge } from "@/components/status-badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { toast } from "@/components/ui/toast"
import {
  api,
  type DashboardResponse,
  type HostConfiguration,
  type HostInfo,
  type PowerStatus,
} from "@/lib/api"
import { formatDateTime, formatUptime } from "@/lib/format"
import { Page, QueryError } from "@/lib/page"

type Editing = "hostname" | "timezone" | null

const timezones = (() => {
  try {
    return Intl.supportedValuesOf("timeZone")
  } catch {
    return ["UTC"]
  }
})()

function HostPage() {
  const dashboard = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api<DashboardResponse>("/dashboard"),
    refetchInterval: 30_000,
  })
  const configuration = useQuery({
    queryKey: ["host", "configuration"],
    queryFn: () => api<HostConfiguration>("/host/config"),
  })
  return (
    <Page description="Identity, time, and power for this machine.">
      <div className="grid gap-6 @4xl/main:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <div className="flex flex-col gap-6">
          {configuration.isPending ? (
            <Skeleton className="h-56" />
          ) : configuration.isError ? (
            <QueryError
              error={configuration.error}
              retry={() => void configuration.refetch()}
            />
          ) : (
            <Settings configuration={configuration.data} />
          )}
          <Power />
        </div>
        <System info={dashboard.data?.host} />
      </div>
    </Page>
  )
}

function Row({
  label,
  children,
  action,
}: {
  label: string
  children: React.ReactNode
  action?: React.ReactNode
}) {
  return (
    <div className="flex min-h-12 items-center gap-4 py-2">
      <span className="w-36 shrink-0 text-sm text-muted-foreground">
        {label}
      </span>
      <span className="min-w-0 flex-1 truncate text-sm">{children}</span>
      {action}
    </div>
  )
}

function useHostClock(timezone: string) {
  const [now, setNow] = React.useState(() => new Date())
  React.useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "medium",
      timeZone: timezone,
    }).format(now)
  } catch {
    return now.toLocaleString()
  }
}

function Settings({ configuration }: { configuration: HostConfiguration }) {
  const { administrative } = useAdminAccess()
  const [editing, setEditing] = React.useState<Editing>(null)
  const clock = useHostClock(configuration.timezone)
  const update = useUpdateConfiguration(configuration)
  return (
    <Card>
      <CardHeader>
        <CardTitle>Identity and time</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col">
        {!administrative && (
          <div className="pb-3">
            <AdminRequired>
              Changing these settings requires administrative access.
            </AdminRequired>
          </div>
        )}
        <Row
          label="Hostname"
          action={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Change hostname"
              disabled={!administrative}
              onClick={() => setEditing("hostname")}
            >
              <PencilIcon />
            </Button>
          }
        >
          <span className="font-medium">{configuration.hostname}</span>
        </Row>
        <Separator />
        <Row
          label="Time zone"
          action={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Change time zone"
              disabled={!administrative}
              onClick={() => setEditing("timezone")}
            >
              <PencilIcon />
            </Button>
          }
        >
          {configuration.timezone}
        </Row>
        <Separator />
        <Row label="Host time">
          <span className="tabular-nums">{clock}</span>
        </Row>
        <Separator />
        <Row
          label="Automatic time"
          action={
            <Switch
              checked={configuration.ntpEnabled}
              disabled={!administrative || update.isPending}
              aria-label="Synchronize time over the network"
              onCheckedChange={(ntpEnabled) => update.mutate({ ntpEnabled })}
            />
          }
        >
          <span className="text-muted-foreground">
            {configuration.ntpEnabled
              ? "Synchronized over the network"
              : "Set manually"}
          </span>
        </Row>
      </CardContent>
      {editing && (
        <EditSetting
          field={editing}
          configuration={configuration}
          onClose={() => setEditing(null)}
        />
      )}
    </Card>
  )
}

function useUpdateConfiguration(configuration: HostConfiguration) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (change: Partial<Omit<HostConfiguration, "fingerprint">>) =>
      api<HostConfiguration>("/host/config", {
        method: "PUT",
        body: JSON.stringify({
          hostname: configuration.hostname,
          timezone: configuration.timezone,
          ntpEnabled: configuration.ntpEnabled,
          ...change,
          expectedFingerprint: configuration.fingerprint,
        }),
      }),
    onSuccess: (value) => {
      client.setQueryData(["host", "configuration"], value)
      void client.invalidateQueries({ queryKey: ["dashboard"] })
      toast.add({ title: "Host settings updated", type: "success" })
    },
    onError: (error) =>
      toast.add({
        title: "Host settings unchanged",
        description: error.message,
        type: "error",
      }),
  })
}

function EditSetting({
  field,
  configuration,
  onClose,
}: {
  field: "hostname" | "timezone"
  configuration: HostConfiguration
  onClose: () => void
}) {
  const update = useUpdateConfiguration(configuration)
  const [value, setValue] = React.useState(configuration[field])
  const valid =
    field === "hostname"
      ? /^(?=.{1,253}$)[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/.test(
          value
        )
      : timezones.includes(value)
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form
          className="flex flex-col gap-5"
          onSubmit={(event) => {
            event.preventDefault()
            update.mutate({ [field]: value.trim() }, { onSuccess: onClose })
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {field === "hostname" ? "Change hostname" : "Change time zone"}
            </DialogTitle>
            <DialogDescription>
              {field === "hostname"
                ? "Other machines and your shell prompt use this name."
                : "Logs and schedules use this zone for local time."}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            {field === "hostname" ? (
              <Field data-invalid={value !== "" && !valid}>
                <FieldLabel htmlFor="hostname">Hostname</FieldLabel>
                <Input
                  id="hostname"
                  autoFocus
                  required
                  maxLength={253}
                  value={value}
                  aria-invalid={value !== "" && !valid}
                  onChange={(event) => setValue(event.target.value)}
                />
                <FieldDescription>
                  Letters, digits, and hyphens; dots separate labels.
                </FieldDescription>
              </Field>
            ) : (
              <Field>
                <FieldLabel htmlFor="timezone">Time zone</FieldLabel>
                <Combobox
                  items={timezones}
                  value={value}
                  onValueChange={(next) => next && setValue(next)}
                >
                  <ComboboxInput
                    id="timezone"
                    placeholder="Search time zones"
                  />
                  <ComboboxContent>
                    <ComboboxEmpty>No time zone found.</ComboboxEmpty>
                    <ComboboxList>
                      {(item: string) => (
                        <ComboboxItem key={item} value={item}>
                          {item.replaceAll("_", " ")}
                        </ComboboxItem>
                      )}
                    </ComboboxList>
                  </ComboboxContent>
                </Combobox>
              </Field>
            )}
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                !valid || value === configuration[field] || update.isPending
              }
            >
              {update.isPending && <Spinner data-icon="inline-start" />}
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function System({ info }: { info?: HostInfo }) {
  if (!info) return <Skeleton className="h-72" />
  const facts: [string, React.ReactNode][] = [
    ["Operating system", info.operatingSystem],
    ["Kernel", info.kernel],
    ["Architecture", info.architecture],
    [
      "Processor",
      info.hardware.cpuModel
        ? `${info.hardware.cpuModel}${info.hardware.cpuCores ? ` · ${info.hardware.cpuCores} threads` : ""}`
        : undefined,
    ],
    ["Up for", formatUptime(info.uptimeSeconds)],
    ["Booted", formatDateTime(info.bootedAt)],
  ]
  return (
    <Card className="h-fit">
      <CardHeader>
        <CardTitle>System</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="flex flex-col gap-3 text-sm">
          {facts
            .filter(([, value]) => value)
            .map(([label, value]) => (
              <div key={label} className="flex flex-col gap-0.5">
                <dt className="text-xs text-muted-foreground">{label}</dt>
                <dd className="break-words">{value}</dd>
              </div>
            ))}
        </dl>
      </CardContent>
    </Card>
  )
}

const powerState: Record<
  PowerStatus["reboot"]["state"],
  { tone: "success" | "warning" | "danger" | "muted"; label: string }
> = {
  available: { tone: "success", label: "Allowed" },
  challenged: { tone: "warning", label: "Needs authentication" },
  inhibited: { tone: "warning", label: "Blocked by a program" },
  denied: { tone: "danger", label: "Denied by policy" },
  unavailable: { tone: "muted", label: "Unavailable" },
}

function Power() {
  const { administrative } = useAdminAccess()
  const query = useQuery({
    queryKey: ["host", "power"],
    queryFn: () => api<PowerStatus>("/host/power"),
    refetchInterval: 30_000,
  })
  const [action, setAction] = React.useState<"reboot" | "shutdown">()
  if (query.isPending) return <Skeleton className="h-40" />
  if (query.isError)
    return <QueryError error={query.error} retry={() => void query.refetch()} />
  const status = query.data
  const inhibitors = status.inhibitors.filter((inhibitor) =>
    inhibitor.what.includes("shutdown")
  )
  return (
    <Card>
      <CardHeader>
        <CardTitle>Power</CardTitle>
        <CardDescription>
          Restarting or shutting down disconnects everyone, including this
          console.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {!status.available && (
          <Alert>
            <AlertTitle>Power management unavailable</AlertTitle>
            <AlertDescription>
              {status.reason || "systemd-logind did not respond."}
            </AlertDescription>
          </Alert>
        )}
        {inhibitors.length > 0 && (
          <Alert>
            <AlertTitle>Programs are delaying shutdown</AlertTitle>
            <AlertDescription>
              <ul className="flex flex-col gap-0.5">
                {inhibitors.slice(0, 6).map((inhibitor) => (
                  <li key={`${inhibitor.pid}-${inhibitor.what}`}>
                    <span className="font-medium text-foreground">
                      {inhibitor.who || `PID ${inhibitor.pid}`}
                    </span>
                    : {inhibitor.why || inhibitor.what}
                  </li>
                ))}
              </ul>
            </AlertDescription>
          </Alert>
        )}
        <div className="flex flex-wrap items-center gap-2">
          {(["reboot", "shutdown"] as const).map((value) => (
            <Button
              key={value}
              variant={value === "shutdown" ? "destructive" : "outline"}
              disabled={!administrative || !status[value].available}
              onClick={() => setAction(value)}
            >
              {value === "reboot" ? (
                <RotateCcwIcon data-icon="inline-start" />
              ) : (
                <PowerIcon data-icon="inline-start" />
              )}
              {value === "reboot" ? "Restart" : "Shut down"}
            </Button>
          ))}
          {status.available && status.reboot.state !== "available" && (
            <StatusBadge tone={powerState[status.reboot.state].tone}>
              {powerState[status.reboot.state].label}
            </StatusBadge>
          )}
        </div>
      </CardContent>
      {action && (
        <ConfirmPower
          action={action}
          status={status}
          onClose={() => setAction(undefined)}
        />
      )}
    </Card>
  )
}

function ConfirmPower({
  action,
  status,
  onClose,
}: {
  action: "reboot" | "shutdown"
  status: PowerStatus
  onClose: () => void
}) {
  const [typed, setTyped] = React.useState("")
  const [requested, setRequested] = React.useState(false)
  const hostname = useQueryClient().getQueryData<HostConfiguration>([
    "host",
    "configuration",
  ])?.hostname
  const word = action.toUpperCase()
  const request = useMutation({
    mutationFn: () =>
      api<{ message: string }>("/host/power", {
        method: "POST",
        body: JSON.stringify({
          action,
          confirmation: word,
          expectedFingerprint: status.fingerprint,
        }),
      }),
    onSuccess: () => setRequested(true),
  })
  return (
    <Dialog open onOpenChange={(open) => !open && !requested && onClose()}>
      <DialogContent showCloseButton={!requested}>
        {requested ? (
          <DialogHeader>
            <DialogTitle>
              {action === "reboot" ? "Restarting…" : "Shutting down…"}
            </DialogTitle>
            <DialogDescription>
              {action === "reboot"
                ? "This page reconnects once the host is back."
                : "The host is going offline. You can close this page."}
            </DialogDescription>
          </DialogHeader>
        ) : (
          <form
            className="flex flex-col gap-5"
            onSubmit={(event) => {
              event.preventDefault()
              request.mutate()
            }}
          >
            <DialogHeader>
              <DialogTitle>
                {action === "reboot" ? "Restart" : "Shut down"}{" "}
                {hostname ?? "this host"}?
              </DialogTitle>
              <DialogDescription>
                Running programs stop and every session is disconnected.
              </DialogDescription>
            </DialogHeader>
            <Field>
              <FieldLabel htmlFor="power-confirm">
                Type <span className="font-mono font-semibold">{word}</span> to
                confirm
              </FieldLabel>
              <Input
                id="power-confirm"
                autoFocus
                autoComplete="off"
                value={typed}
                onChange={(event) => setTyped(event.target.value)}
              />
            </Field>
            {request.isError && (
              <Alert variant="destructive">
                <AlertTitle>Request refused</AlertTitle>
                <AlertDescription>{request.error.message}</AlertDescription>
              </Alert>
            )}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button
                type="submit"
                variant="destructive"
                disabled={
                  typed.trim().toUpperCase() !== word || request.isPending
                }
              >
                {request.isPending && <Spinner data-icon="inline-start" />}
                {action === "reboot" ? "Restart" : "Shut down"}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

export const Route = createFileRoute("/host")({ component: HostPage })
