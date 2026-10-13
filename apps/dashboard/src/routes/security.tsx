import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import {
  ArrowRightIcon,
  SearchIcon,
  ShieldCheckIcon,
  ShieldIcon,
} from "lucide-react"
import * as React from "react"

import { useAdminAccess } from "@/components/admin-access"
import { StatusBadge } from "@/components/status-badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { toast } from "@/components/ui/toast"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { api, type SecurityOperation, type SecurityStatus } from "@/lib/api"
import { Page, QueryError } from "@/lib/page"

type Change = Omit<SecurityOperation, "expectedFingerprint" | "confirmation">

const severityTone = {
  critical: "danger",
  high: "danger",
  error: "danger",
  warning: "warning",
  medium: "warning",
} as const

function SecurityPage() {
  const query = useQuery({
    queryKey: ["security"],
    queryFn: () => api<SecurityStatus>("/security"),
    refetchInterval: 30_000,
  })
  const [change, setChange] = React.useState<Change>()
  return (
    <Page description="Mandatory access control: SELinux and AppArmor.">
      {query.isPending ? (
        <Skeleton className="h-48" />
      ) : query.isError ? (
        <QueryError error={query.error} retry={() => void query.refetch()} />
      ) : (
        <>
          <Overview status={query.data} />
          {query.data.findings.length > 0 && (
            <div className="flex flex-col gap-2">
              {query.data.findings.map((finding) => (
                <Alert
                  key={`${finding.framework}:${finding.kind}:${finding.subject}`}
                  variant={
                    severityTone[
                      finding.severity as keyof typeof severityTone
                    ] === "danger"
                      ? "destructive"
                      : "default"
                  }
                >
                  <AlertTitle>{finding.message}</AlertTitle>
                  {finding.guidance && (
                    <AlertDescription>{finding.guidance}</AlertDescription>
                  )}
                </Alert>
              ))}
            </div>
          )}
          {query.data.selinux.userspace && (
            <SELinux status={query.data} onChange={setChange} />
          )}
          {query.data.apparmor.userspace && (
            <AppArmor status={query.data} onChange={setChange} />
          )}
          <Denials status={query.data} />
          {change && (
            <ConfirmChange
              change={change}
              onClose={() => setChange(undefined)}
            />
          )}
        </>
      )}
    </Page>
  )
}

function Overview({ status }: { status: SecurityStatus }) {
  const mode = status.selinux.mode.toLowerCase()
  const modes = Object.values(status.apparmor.profileModes ?? {})
  const enforcing = modes.filter((value) => value === "enforce").length
  if (!status.selinux.kernelPresent && !status.apparmor.kernelPresent) {
    return (
      <Empty className="border">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ShieldIcon />
          </EmptyMedia>
          <EmptyTitle>No mandatory access control</EmptyTitle>
          <EmptyDescription>
            Neither SELinux nor AppArmor is enabled in this kernel.
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <div className="grid gap-4 @3xl/main:grid-cols-2">
      {status.selinux.kernelPresent && (
        <Card size="sm">
          <CardHeader>
            <CardDescription>SELinux</CardDescription>
            <CardTitle className="text-xl capitalize">
              {mode || "Unknown"}
            </CardTitle>
            <CardAction>
              <StatusBadge
                tone={
                  mode === "enforcing"
                    ? "success"
                    : mode === "permissive"
                      ? "warning"
                      : "muted"
                }
              >
                {mode === "enforcing"
                  ? "Protected"
                  : mode === "permissive"
                    ? "Logging only"
                    : "Off"}
              </StatusBadge>
            </CardAction>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            {status.selinux.policy
              ? `Policy ${status.selinux.policy}`
              : "Policy unknown"}{" "}
            · {status.selinux.booleans.length} booleans
          </CardContent>
        </Card>
      )}
      {status.apparmor.kernelPresent && (
        <Card size="sm">
          <CardHeader>
            <CardDescription>AppArmor</CardDescription>
            <CardTitle className="text-xl">
              {status.apparmor.profiles.length} profiles
            </CardTitle>
            <CardAction>
              <StatusBadge
                tone={status.apparmor.userspace ? "success" : "muted"}
              >
                {status.apparmor.userspace ? "Active" : "Tools missing"}
              </StatusBadge>
            </CardAction>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            {enforcing} enforcing · {modes.length - enforcing} other
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function SELinux({
  status,
  onChange,
}: {
  status: SecurityStatus
  onChange: (change: Change) => void
}) {
  const { administrative } = useAdminAccess()
  const [filter, setFilter] = React.useState("")
  const [path, setPath] = React.useState("")
  const booleans = status.selinux.booleans
    .map((entry) => {
      const [name, value] = entry.split("=")
      return { name, on: value === "on" || value === "1" || value === "true" }
    })
    .filter((item) => item.name.includes(filter.trim().toLowerCase()))
  return (
    <Card>
      <CardHeader>
        <CardTitle>SELinux booleans</CardTitle>
        <CardDescription>
          Switches that turn optional policy rules on or off. Changes persist
          across reboots.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <InputGroup>
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            placeholder="Filter booleans"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
        </InputGroup>
        <ul className="grid max-h-96 overflow-auto rounded-lg border @3xl/main:grid-cols-2">
          {booleans.map((item) => (
            <li
              key={item.name}
              className="flex items-center justify-between gap-3 border-b px-3 py-2 last:border-b-0"
            >
              <span className="truncate font-mono text-xs">{item.name}</span>
              <Switch
                checked={item.on}
                disabled={!administrative}
                aria-label={item.name}
                onCheckedChange={(value) =>
                  onChange({
                    framework: "SELinux",
                    action: "selinux-boolean",
                    boolean: item.name,
                    value,
                  })
                }
              />
            </li>
          ))}
          {!booleans.length && (
            <li className="p-3 text-sm text-muted-foreground">
              No matching booleans.
            </li>
          )}
        </ul>
        <form
          className="flex flex-col gap-2 pt-2"
          onSubmit={(event) => {
            event.preventDefault()
            onChange({
              framework: "SELinux",
              action: "selinux-restorecon",
              path: path.trim(),
            })
          }}
        >
          <Field>
            <FieldLabel htmlFor="restorecon-path">
              Restore file labels
            </FieldLabel>
            <div className="flex gap-2">
              <Input
                id="restorecon-path"
                required
                className="font-mono"
                placeholder="/var/www/html"
                value={path}
                onChange={(event) => setPath(event.target.value)}
              />
              <Button
                type="submit"
                variant="outline"
                disabled={!administrative || !path.startsWith("/")}
              >
                Restore
              </Button>
            </div>
            <FieldDescription>
              Resets one path to the label the policy expects.
            </FieldDescription>
          </Field>
        </form>
      </CardContent>
    </Card>
  )
}

function AppArmor({
  status,
  onChange,
}: {
  status: SecurityStatus
  onChange: (change: Change) => void
}) {
  const { administrative } = useAdminAccess()
  const [filter, setFilter] = React.useState("")
  const [path, setPath] = React.useState("")
  const profiles = status.apparmor.profiles.filter((profile) =>
    profile.toLowerCase().includes(filter.trim().toLowerCase())
  )
  return (
    <Card>
      <CardHeader>
        <CardTitle>AppArmor profiles</CardTitle>
        <CardDescription>
          Enforce blocks what a profile forbids; complain only logs it.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <InputGroup>
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            placeholder="Filter profiles"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
        </InputGroup>
        <ul className="flex max-h-96 flex-col divide-y overflow-auto rounded-lg border">
          {profiles.map((profile) => {
            const mode = status.apparmor.profileModes?.[profile] ?? "unknown"
            return (
              <li
                key={profile}
                className="flex items-center justify-between gap-3 px-3 py-1.5"
              >
                <span className="min-w-0 truncate font-mono text-xs">
                  {profile}
                </span>
                <ToggleGroup
                  value={[mode]}
                  size="sm"
                  variant="outline"
                  disabled={!administrative}
                  onValueChange={(next) =>
                    (next[0] === "enforce" || next[0] === "complain") &&
                    next[0] !== mode &&
                    onChange({
                      framework: "AppArmor",
                      action: `apparmor-${next[0]}`,
                      profile,
                    })
                  }
                >
                  <ToggleGroupItem value="enforce">Enforce</ToggleGroupItem>
                  <ToggleGroupItem value="complain">Complain</ToggleGroupItem>
                </ToggleGroup>
              </li>
            )
          })}
          {!profiles.length && (
            <li className="p-3 text-sm text-muted-foreground">
              No matching profiles.
            </li>
          )}
        </ul>
        <form
          className="pt-2"
          onSubmit={(event) => {
            event.preventDefault()
            onChange({
              framework: "AppArmor",
              action: "apparmor-load",
              path: path.trim(),
            })
          }}
        >
          <Field>
            <FieldLabel htmlFor="apparmor-path">
              Load or reload a profile
            </FieldLabel>
            <div className="flex gap-2">
              <Input
                id="apparmor-path"
                required
                className="font-mono"
                placeholder="/etc/apparmor.d/usr.sbin.nginx"
                value={path}
                onChange={(event) => setPath(event.target.value)}
              />
              <Button
                type="submit"
                variant="outline"
                disabled={!administrative || !path.startsWith("/")}
              >
                Load
              </Button>
            </div>
          </Field>
        </form>
      </CardContent>
    </Card>
  )
}

function Denials({ status }: { status: SecurityStatus }) {
  const denials = [
    ...status.selinux.denials.map((text) => ({ framework: "SELinux", text })),
    ...status.apparmor.denials.map((text) => ({ framework: "AppArmor", text })),
  ]
  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent denials</CardTitle>
        <CardDescription>
          Access the policy blocked or would have blocked.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {denials.length ? (
          <ul className="flex max-h-80 flex-col gap-1.5 overflow-auto">
            {denials.map((denial, index) => (
              <li
                // biome-ignore lint/suspicious/noArrayIndexKey: identical denial lines can repeat in a static list
                key={`${denial.framework}-${index}`}
                className="flex items-start gap-2"
              >
                <Badge variant="outline" className="shrink-0 font-normal">
                  {denial.framework}
                </Badge>
                <code className="text-xs break-all text-muted-foreground">
                  {denial.text}
                </code>
              </li>
            ))}
          </ul>
        ) : (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <ShieldCheckIcon className="size-4 text-success" />
            Nothing was denied recently.
          </p>
        )}
      </CardContent>
    </Card>
  )
}

// ConfirmChange previews one change against the current policy, then applies
// exactly that change.
function ConfirmChange({
  change,
  onClose,
}: {
  change: Change
  onClose: () => void
}) {
  const client = useQueryClient()
  const preview = useQuery({
    queryKey: ["security", "preview", change],
    queryFn: () =>
      api<SecurityStatus>("/security/preview", {
        method: "POST",
        body: JSON.stringify(change),
      }),
    gcTime: 0,
  })
  const apply = useMutation({
    mutationFn: () =>
      api<SecurityStatus>("/security", {
        method: "POST",
        body: JSON.stringify({
          ...change,
          expectedFingerprint: preview.data?.fingerprint,
          confirmation: "CONFIRM NARROW SECURITY CHANGE",
        } satisfies SecurityOperation),
      }),
    onSuccess: (status) => {
      client.setQueryData(["security"], status)
      toast.add({ title: "Security policy updated", type: "success" })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Change security policy?</DialogTitle>
          <DialogDescription>
            Review the change before it is applied to the running system.
          </DialogDescription>
        </DialogHeader>
        {preview.isPending ? (
          <Skeleton className="h-16" />
        ) : preview.isError ? (
          <Alert variant="destructive">
            <AlertTitle>Preview failed</AlertTitle>
            <AlertDescription>{preview.error.message}</AlertDescription>
          </Alert>
        ) : (
          <FieldGroup className="gap-3">
            {preview.data.changes?.map((item) => (
              <div
                key={item.field}
                className="flex flex-col gap-1 rounded-lg border p-3 text-sm"
              >
                <span className="font-medium">{item.field}</span>
                <span className="flex flex-wrap items-center gap-2 font-mono text-xs text-muted-foreground">
                  {item.before && (
                    <>
                      {item.before}
                      <ArrowRightIcon className="size-3" />
                    </>
                  )}
                  <span className="text-foreground">{item.after}</span>
                </span>
              </div>
            ))}
            {preview.data.warnings?.map((warning) => (
              <p key={warning} className="text-sm text-muted-foreground">
                {warning}
              </p>
            ))}
          </FieldGroup>
        )}
        {apply.isError && (
          <Alert variant="destructive">
            <AlertTitle>Nothing changed</AlertTitle>
            <AlertDescription>{apply.error.message}</AlertDescription>
          </Alert>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            disabled={
              !preview.data?.allowed || preview.data.stale || apply.isPending
            }
            onClick={() => apply.mutate()}
          >
            {apply.isPending && <Spinner data-icon="inline-start" />}
            Apply
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export const Route = createFileRoute("/security")({ component: SecurityPage })
