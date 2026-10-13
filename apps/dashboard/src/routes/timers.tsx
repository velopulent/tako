import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import { PlusIcon } from "lucide-react"
import * as React from "react"

import { AdminRequired, useAdminAccess } from "@/components/admin-access"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { StatusBadge, unitStatus } from "@/components/status-badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
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
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { toast } from "@/components/ui/toast"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  APIError,
  api,
  type ServiceInfo,
  type TimerOperation,
  type TimerState,
} from "@/lib/api"
import { formatDateTime, formatRelative } from "@/lib/format"
import { Page, QueryError, State, usePageInterval } from "@/lib/page"
import { parseEnum } from "@/lib/search"
import { useSession } from "@/lib/session"

type Scope = "system" | "user"

const columns: ColumnDef<DataTableFeatures, ServiceInfo>[] = [
  {
    accessorKey: "name",
    header: "Timer",
    size: 260,
    cell: ({ row }) => (
      <span className="flex min-w-0 flex-col">
        <span className="truncate font-medium">
          {row.original.name.replace(/\.timer$/, "")}
        </span>
        <span className="truncate text-xs text-muted-foreground">
          {row.original.description}
        </span>
      </span>
    ),
  },
  {
    id: "next",
    accessorFn: (row) =>
      row.nextRun ? Date.parse(row.nextRun) : Number.MAX_SAFE_INTEGER,
    header: "Next run",
    size: 150,
    cell: ({ row }) =>
      row.original.nextRun ? (
        <span title={formatDateTime(row.original.nextRun)}>
          {formatRelative(row.original.nextRun)}
        </span>
      ) : (
        <span className="text-muted-foreground">Not scheduled</span>
      ),
  },
  {
    id: "last",
    accessorFn: (row) => (row.lastRun ? Date.parse(row.lastRun) : 0),
    header: "Last run",
    size: 140,
    cell: ({ row }) =>
      row.original.lastRun ? (
        <span title={formatDateTime(row.original.lastRun)}>
          {formatRelative(row.original.lastRun)}
        </span>
      ) : (
        <span className="text-muted-foreground">Never</span>
      ),
  },
  {
    accessorKey: "activates",
    header: "Runs",
    size: 200,
    cell: ({ row }) => (
      <span className="truncate font-mono text-xs">
        {row.original.activates || "—"}
      </span>
    ),
  },
  {
    accessorKey: "activeState",
    header: "Status",
    size: 120,
    cell: ({ row }) => {
      const status = unitStatus(row.original.activeState, row.original.subState)
      return (
        <StatusBadge tone={status.tone}>
          {row.original.activeState === "active" ? "Scheduled" : status.label}
        </StatusBadge>
      )
    },
  },
]

function TimersPage() {
  const interval = usePageInterval("timers")
  const search = Route.useSearch()
  const navigate = useNavigate({ from: "/timers" })
  const scope: Scope = search.scope ?? "system"
  const [editing, setEditing] = React.useState<string | null>(null)
  const query = useQuery({
    queryKey: ["services", scope, "timer"],
    queryFn: () =>
      api<{ items: ServiceInfo[] }>(`/services?scope=${scope}&type=timer`),
    refetchInterval: interval.milliseconds,
  })
  return (
    <Page
      description="Scheduled jobs run by systemd. Tako creates and edits timers in /etc/systemd/system or your user configuration."
      interval={interval}
      actions={
        <>
          <ToggleGroup
            value={[scope]}
            onValueChange={(value) =>
              value[0] &&
              navigate({
                search: { scope: value[0] === "user" ? "user" : undefined },
                replace: true,
              })
            }
            variant="outline"
            size="sm"
          >
            <ToggleGroupItem value="system">System</ToggleGroupItem>
            <ToggleGroupItem value="user">My session</ToggleGroupItem>
          </ToggleGroup>
          <Button size="sm" onClick={() => setEditing("")}>
            <PlusIcon data-icon="inline-start" />
            New timer
          </Button>
        </>
      }
    >
      <State query={query} empty={false}>
        <DataTable
          data={query.data?.items ?? []}
          columns={columns}
          height="calc(100svh - 14rem)"
          initialSorting={[{ id: "next", desc: false }]}
          searchPlaceholder="Search timers"
          emptyMessage="No timers in this scope."
          onRowClick={(row) => setEditing(row.name.replace(/\.timer$/, ""))}
        />
      </State>
      {editing !== null && (
        <TimerSheet
          scope={scope}
          name={editing}
          onClose={() => setEditing(null)}
        />
      )}
    </Page>
  )
}

type ScheduleKind = "calendar" | "boot" | "active"

type Draft = {
  name: string
  description: string
  kind: ScheduleKind
  schedule: string
  command: string
  persistent: boolean
}

const calendarPresets = [
  { label: "Hourly", value: "hourly" },
  { label: "Daily", value: "daily" },
  { label: "Weekly", value: "weekly" },
  { label: "Monthly", value: "monthly" },
]

const kindItems = [
  { value: "calendar", label: "On a calendar schedule" },
  { value: "boot", label: "After boot" },
  { value: "active", label: "Repeatedly, after each run" },
]

function draftFromState(name: string, state?: TimerState): Draft {
  const definition = state?.definition
  const kind: ScheduleKind = definition?.onBootSec
    ? "boot"
    : definition?.onUnitActiveSec
      ? "active"
      : "calendar"
  return {
    name,
    description: definition?.description ?? "",
    kind,
    schedule:
      definition?.onCalendar ??
      definition?.onBootSec ??
      definition?.onUnitActiveSec ??
      (state?.exists ? "" : "daily"),
    command: definition?.command ?? "",
    persistent: definition?.persistent ?? true,
  }
}

function operationFrom(
  scope: Scope,
  draft: Draft,
  action: TimerOperation["action"],
  fingerprint?: string
): TimerOperation {
  return {
    action,
    scope,
    name: draft.name.trim(),
    description: draft.description.trim() || undefined,
    onCalendar: draft.kind === "calendar" ? draft.schedule.trim() : undefined,
    onBootSec: draft.kind === "boot" ? draft.schedule.trim() : undefined,
    onUnitActiveSec:
      draft.kind === "active" ? draft.schedule.trim() : undefined,
    command: draft.command.trim(),
    persistent: draft.kind === "calendar" ? draft.persistent : undefined,
    expectedFingerprint: fingerprint,
  }
}

function timerError(error: unknown) {
  if (error instanceof APIError) {
    if (error.code === "timer-conflict")
      return "This timer changed somewhere else. Close and reopen it to load the latest version."
    if (error.code === "invalid-timer-operation")
      return "Check the name (letters, digits, . _ - @), the schedule, and that the command starts with an absolute path and has no shell characters."
    return error.message
  }
  return "The timer could not be saved."
}

function TimerSheet({
  scope,
  name,
  onClose,
}: {
  scope: Scope
  name: string
  onClose: () => void
}) {
  const client = useQueryClient()
  const session = useSession()
  const { administrative } = useAdminAccess()
  const creating = name === ""
  const canWrite = scope === "user" ? Boolean(session.user) : administrative
  const state = useQuery({
    queryKey: ["timer", scope, name],
    queryFn: () =>
      api<TimerState>("/timers/preview", {
        method: "POST",
        body: JSON.stringify({ action: "preview", scope, name }),
      }),
    enabled: !creating && canWrite,
  })
  const [draft, setDraft] = React.useState<Draft>(() => draftFromState(name))
  React.useEffect(() => {
    if (state.data) setDraft(draftFromState(name, state.data))
  }, [state.data, name])

  const change = useMutation({
    mutationFn: (operation: TimerOperation) =>
      api<TimerState>("/timers", {
        method: "POST",
        body: JSON.stringify(operation),
      }),
    onSuccess: (_, operation) => {
      void client.invalidateQueries({ queryKey: ["services", scope, "timer"] })
      const verb = {
        create: "created",
        update: "saved",
        enable: "enabled",
        disable: "disabled",
        delete: "deleted",
        preview: "",
      }[operation.action]
      toast.add({ title: `Timer ${operation.name} ${verb}`, type: "success" })
      onClose()
    },
  })
  const update = (patch: Partial<Draft>) =>
    setDraft((current) => ({ ...current, ...patch }))
  const managed = creating || state.data?.exists
  const fingerprint = state.data?.fingerprint
  const pending = change.isPending

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="w-full gap-0 sm:max-w-lg">
        <SheetHeader className="border-b">
          <SheetTitle>{creating ? "New timer" : name}</SheetTitle>
          <SheetDescription>
            {creating
              ? "Creates a timer and the service it runs."
              : state.data?.enabled
                ? "Enabled · starts automatically"
                : "Disabled"}
          </SheetDescription>
        </SheetHeader>
        <div className="flex-1 overflow-auto p-4">
          {!canWrite ? (
            <AdminRequired>
              Creating and editing system timers requires administrative access.
            </AdminRequired>
          ) : state.isPending && !creating ? (
            <Skeleton className="h-96" />
          ) : state.isError ? (
            <QueryError
              error={state.error}
              retry={() => void state.refetch()}
            />
          ) : !managed ? (
            <Alert>
              <AlertTitle>Provided by a package</AlertTitle>
              <AlertDescription>
                This timer is installed by the operating system, so Tako does
                not edit it. You can still start, stop, or disable it from{" "}
                <Link
                  to="/services/$scope/$unit"
                  params={{ scope, unit: `${name}.timer` }}
                  className="text-primary underline-offset-4 hover:underline"
                >
                  its unit page
                </Link>
                .
              </AlertDescription>
            </Alert>
          ) : (
            <form
              id="timer-form"
              onSubmit={(event) => {
                event.preventDefault()
                change.mutate(
                  operationFrom(
                    scope,
                    draft,
                    creating ? "create" : "update",
                    creating ? undefined : fingerprint
                  )
                )
              }}
            >
              <FieldGroup>
                {change.isError && (
                  <Alert variant="destructive">
                    <AlertTitle>Timer not saved</AlertTitle>
                    <AlertDescription>
                      {timerError(change.error)}
                    </AlertDescription>
                  </Alert>
                )}
                {creating && (
                  <Field>
                    <FieldLabel htmlFor="timer-name">Name</FieldLabel>
                    <Input
                      id="timer-name"
                      required
                      maxLength={64}
                      pattern="[A-Za-z0-9@._\-]+"
                      placeholder="nightly-backup"
                      value={draft.name}
                      onChange={(event) => update({ name: event.target.value })}
                    />
                    <FieldDescription>
                      Creates {draft.name || "name"}.timer and{" "}
                      {draft.name || "name"}.service.
                    </FieldDescription>
                  </Field>
                )}
                <Field>
                  <FieldLabel htmlFor="timer-description">
                    Description
                  </FieldLabel>
                  <Input
                    id="timer-description"
                    maxLength={256}
                    placeholder="Back up the database"
                    value={draft.description}
                    onChange={(event) =>
                      update({ description: event.target.value })
                    }
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="timer-command">Command</FieldLabel>
                  <Input
                    id="timer-command"
                    required
                    maxLength={512}
                    className="font-mono"
                    placeholder="/usr/local/bin/backup --all"
                    value={draft.command}
                    onChange={(event) =>
                      update({ command: event.target.value })
                    }
                  />
                  <FieldDescription>
                    An absolute path and its arguments. Shell features such as
                    pipes and variables are not allowed.
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="timer-kind">Runs</FieldLabel>
                  <Select
                    items={kindItems}
                    value={draft.kind}
                    onValueChange={(kind) =>
                      update({
                        kind: kind as ScheduleKind,
                        schedule: kind === "calendar" ? "daily" : "15min",
                      })
                    }
                  >
                    <SelectTrigger id="timer-kind">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {kindItems.map((item) => (
                          <SelectItem key={item.value} value={item.value}>
                            {item.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                <Field>
                  <FieldLabel htmlFor="timer-schedule">
                    {draft.kind === "calendar"
                      ? "Calendar expression"
                      : "Delay"}
                  </FieldLabel>
                  <Input
                    id="timer-schedule"
                    required
                    maxLength={256}
                    className="font-mono"
                    value={draft.schedule}
                    onChange={(event) =>
                      update({ schedule: event.target.value })
                    }
                  />
                  {draft.kind === "calendar" ? (
                    <div className="flex flex-wrap gap-1.5">
                      {calendarPresets.map((preset) => (
                        <Button
                          key={preset.value}
                          type="button"
                          variant={
                            draft.schedule === preset.value
                              ? "default"
                              : "outline"
                          }
                          size="xs"
                          onClick={() => update({ schedule: preset.value })}
                        >
                          {preset.label}
                        </Button>
                      ))}
                      <Button
                        type="button"
                        variant={
                          draft.schedule === "*-*-* 03:00:00"
                            ? "default"
                            : "outline"
                        }
                        size="xs"
                        onClick={() => update({ schedule: "*-*-* 03:00:00" })}
                      >
                        Daily at 03:00
                      </Button>
                    </div>
                  ) : (
                    <FieldDescription>
                      A duration such as 30s, 15min, 1h, or 2d.
                    </FieldDescription>
                  )}
                </Field>
                {draft.kind === "calendar" && (
                  <Field orientation="horizontal">
                    <Switch
                      id="timer-persistent"
                      checked={draft.persistent}
                      onCheckedChange={(checked) =>
                        update({ persistent: checked })
                      }
                    />
                    <FieldLabel
                      htmlFor="timer-persistent"
                      className="flex-col items-start gap-0.5"
                    >
                      Catch up after downtime
                      <span className="text-xs font-normal text-muted-foreground">
                        Run once at boot if a scheduled run was missed while the
                        host was off.
                      </span>
                    </FieldLabel>
                  </Field>
                )}
              </FieldGroup>
            </form>
          )}
        </div>
        {canWrite && managed && (creating || state.isSuccess) && (
          <SheetFooter className="flex-row flex-wrap justify-between border-t">
            {!creating && fingerprint ? (
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={pending}
                  onClick={() =>
                    change.mutate({
                      action: state.data?.enabled ? "disable" : "enable",
                      scope,
                      name,
                      expectedFingerprint: fingerprint,
                    })
                  }
                >
                  {state.data?.enabled ? "Disable" : "Enable"}
                </Button>
                <AlertDialog>
                  <AlertDialogTrigger
                    render={
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="text-destructive"
                        disabled={pending}
                      />
                    }
                  >
                    Delete
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>Delete {name}?</AlertDialogTitle>
                      <AlertDialogDescription>
                        The timer stops and both {name}.timer and {name}.service
                        are removed.
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>Cancel</AlertDialogCancel>
                      <AlertDialogAction
                        variant="destructive"
                        onClick={() =>
                          change.mutate({
                            action: "delete",
                            scope,
                            name,
                            expectedFingerprint: fingerprint,
                          })
                        }
                      >
                        Delete timer
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            ) : (
              <span />
            )}
            <Button type="submit" form="timer-form" disabled={pending}>
              {pending && <Spinner data-icon="inline-start" />}
              {creating ? "Create timer" : "Save changes"}
            </Button>
          </SheetFooter>
        )}
      </SheetContent>
    </Sheet>
  )
}

export const Route = createFileRoute("/timers")({
  validateSearch: (search: Record<string, unknown>): { scope?: "user" } =>
    parseEnum(search.scope, ["system", "user"] as const, "system") === "user"
      ? { scope: "user" }
      : {},
  component: TimersPage,
})
