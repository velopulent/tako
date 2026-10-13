import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import { HardDriveIcon, MoreHorizontalIcon } from "lucide-react"
import * as React from "react"

import { AdminRequired, useAdminAccess } from "@/components/admin-access"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { StorageMetrics } from "@/components/directional-metrics"
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  api,
  type FileSystemInfo,
  type MetricSample,
  type StorageDevice,
  type StorageInventoryResponse,
  type StorageOperation,
  type StoragePartition,
  type StorageState,
} from "@/lib/api"
import { formatBytes } from "@/lib/format"
import { Page, State, usePageInterval } from "@/lib/page"
import { cn } from "@/lib/utils"

const columns: ColumnDef<DataTableFeatures, FileSystemInfo>[] = [
  {
    id: "mount",
    header: "Mounted at",
    size: 220,
    accessorFn: (row) => row.targets.map((target) => target.target).join(" "),
    cell: ({ row }) => (
      <span className="flex min-w-0 flex-col">
        <span className="truncate font-medium">
          {row.original.targets[0]?.target ?? "—"}
        </span>
        {row.original.targets.length > 1 && (
          <span className="truncate text-xs text-muted-foreground">
            and {row.original.targets.length - 1} more
          </span>
        )}
      </span>
    ),
  },
  {
    accessorKey: "device",
    header: "Device",
    size: 220,
    cell: ({ row }) => (
      <span className="truncate font-mono text-xs">{row.original.device}</span>
    ),
  },
  {
    accessorKey: "type",
    header: "Type",
    size: 130,
    cell: ({ row }) => (
      <span className="flex gap-1">
        <Badge variant="outline" className="font-normal">
          {row.original.type}
        </Badge>
        {row.original.readOnly && (
          <Badge variant="secondary" className="font-normal">
            read-only
          </Badge>
        )}
      </span>
    ),
  },
  {
    accessorKey: "percent",
    header: "Usage",
    size: 260,
    cell: ({ row }) =>
      row.original.total > 0 ? (
        <div className="flex w-full items-center gap-3">
          <Progress
            value={row.original.percent}
            className={cn(
              "flex-1",
              row.original.percent >= 90 &&
                "[&_[data-slot=progress-indicator]]:bg-destructive"
            )}
            aria-label={`${row.original.percent.toFixed(0)}% used`}
          />
          <span className="w-28 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
            {formatBytes(row.original.used)} / {formatBytes(row.original.total)}
          </span>
        </div>
      ) : (
        <span className="text-xs text-muted-foreground">—</span>
      ),
  },
  {
    accessorKey: "available",
    header: "Free",
    meta: { align: "end" },
    size: 100,
    cell: ({ row }) => formatBytes(row.original.available),
  },
]

function StoragePage() {
  const interval = usePageInterval("storage")
  const [scope, setScope] = React.useState<"local" | "network" | "all">("local")
  const metrics = useQuery({
    queryKey: ["metrics", "1h"],
    queryFn: () => api<{ samples: MetricSample[] }>("/metrics?range=1h"),
  })
  const query = useQuery({
    queryKey: ["storage"],
    queryFn: () => api<StorageInventoryResponse>("/storage"),
    refetchInterval: interval.milliseconds,
  })
  const filesystems = (query.data?.items ?? []).filter(
    (item) => scope === "all" || item.network === (scope === "network")
  )
  return (
    <Page
      description="Disk activity, mounted filesystems, and drives."
      interval={interval}
    >
      <StorageMetrics
        samples={metrics.data?.samples ?? []}
        interval={interval.value}
        pending={metrics.isPending}
        error={metrics.isError}
      />
      <Card>
        <CardHeader>
          <CardTitle>Filesystems</CardTitle>
          <CardAction>
            <ToggleGroup
              value={[scope]}
              onValueChange={(value) =>
                value[0] && setScope(value[0] as typeof scope)
              }
              variant="outline"
              size="sm"
            >
              <ToggleGroupItem value="local">Local</ToggleGroupItem>
              <ToggleGroupItem value="network">Network</ToggleGroupItem>
              <ToggleGroupItem value="all">All</ToggleGroupItem>
            </ToggleGroup>
          </CardAction>
        </CardHeader>
        <CardContent>
          <State query={query} empty={false}>
            <DataTable
              data={filesystems}
              columns={columns}
              height="50vh"
              searchPlaceholder="Search mounts and devices"
              emptyMessage="No filesystems in this view."
              initialSorting={[{ id: "percent", desc: true }]}
            />
          </State>
        </CardContent>
      </Card>
      {query.data && <Drives snapshot={query.data} />}
    </Page>
  )
}

function deviceHealth(device: StorageDevice) {
  if (
    device.smart?.failing ||
    device.smart?.passed === false ||
    device.nvme?.criticalWarning
  )
    return { tone: "danger" as const, label: "Failing" }
  if (device.smart?.available || device.nvme?.available)
    return { tone: "success" as const, label: "Healthy" }
  return { tone: "muted" as const, label: "Health unknown" }
}

function Drives({ snapshot }: { snapshot: StorageInventoryResponse }) {
  const { administrative } = useAdminAccess()
  const [change, setChange] = React.useState<{
    partition: StoragePartition
    action: StorageOperation["action"]
  } | null>(null)
  if (!snapshot.devices.length) {
    if (!administrative)
      return (
        <AdminRequired>
          Drive models, health, and partitions require administrative access.
        </AdminRequired>
      )
    return snapshot.reason ? (
      <Alert>
        <AlertTitle>Drive details unavailable</AlertTitle>
        <AlertDescription>{snapshot.reason}</AlertDescription>
      </Alert>
    ) : null
  }
  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-sm font-medium">Drives</h3>
      {!administrative && (
        <AdminRequired>
          Mounting and unmounting drives requires administrative access.
        </AdminRequired>
      )}
      <div className="grid gap-4 @4xl/main:grid-cols-2">
        {snapshot.devices.map((device) => {
          const health = deviceHealth(device)
          return (
            <Card key={device.path} size="sm">
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <HardDriveIcon className="size-4 text-muted-foreground" />
                  <span className="truncate">
                    {device.model || device.name}
                  </span>
                </CardTitle>
                <CardDescription className="font-mono text-xs">
                  {device.path} · {formatBytes(device.size)}
                  {device.transport ? ` · ${device.transport}` : ""}
                </CardDescription>
                <CardAction className="flex gap-1.5">
                  {device.removable && (
                    <Badge variant="secondary">Removable</Badge>
                  )}
                  <StatusBadge tone={health.tone}>{health.label}</StatusBadge>
                </CardAction>
              </CardHeader>
              <CardContent className="flex flex-col gap-3">
                {(device.nvme?.available || device.smart?.temperatureC) && (
                  <p className="text-xs text-muted-foreground">
                    {[
                      (device.nvme?.temperatureC ??
                        device.smart?.temperatureC) !== undefined &&
                        `${device.nvme?.temperatureC ?? device.smart?.temperatureC} °C`,
                      device.nvme?.percentageUsed !== undefined &&
                        `${device.nvme.percentageUsed}% of rated endurance used`,
                      device.smart?.powerOnHours !== undefined &&
                        `${device.smart.powerOnHours.toLocaleString()} hours powered on`,
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </p>
                )}
                {device.partitions.length ? (
                  <ul className="flex flex-col divide-y rounded-lg border">
                    {device.partitions.map((partition) => (
                      <li
                        key={partition.path}
                        className="flex items-center gap-3 px-3 py-2 text-sm"
                      >
                        <span className="min-w-0 flex-1">
                          <span className="block truncate font-mono text-xs">
                            {partition.path}
                          </span>
                          <span className="block truncate text-xs text-muted-foreground">
                            {[
                              partition.label,
                              partition.filesystem || "no filesystem",
                              formatBytes(partition.size),
                            ]
                              .filter(Boolean)
                              .join(" · ")}
                          </span>
                        </span>
                        <span className="hidden max-w-40 truncate text-xs text-muted-foreground sm:block">
                          {partition.mountPoints.length
                            ? partition.mountPoints
                                .map((mount) => mount.target)
                                .join(", ")
                            : "Not mounted"}
                        </span>
                        <DropdownMenu>
                          <DropdownMenuTrigger
                            render={
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label={`Actions for ${partition.path}`}
                                disabled={
                                  !administrative ||
                                  snapshot.readOnly ||
                                  !partition.filesystem
                                }
                              />
                            }
                          >
                            <MoreHorizontalIcon />
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuGroup>
                              {partition.mountPoints.length ? (
                                <DropdownMenuItem
                                  onClick={() =>
                                    setChange({ partition, action: "unmount" })
                                  }
                                >
                                  Unmount
                                </DropdownMenuItem>
                              ) : (
                                <DropdownMenuItem
                                  onClick={() =>
                                    setChange({ partition, action: "mount" })
                                  }
                                >
                                  Mount
                                </DropdownMenuItem>
                              )}
                              <DropdownMenuItem
                                onClick={() =>
                                  setChange({
                                    partition,
                                    action: "persistent-mount",
                                  })
                                }
                              >
                                Mount at boot…
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                onClick={() =>
                                  setChange({
                                    partition,
                                    action: "persistent-unmount",
                                  })
                                }
                              >
                                Stop mounting at boot…
                              </DropdownMenuItem>
                            </DropdownMenuGroup>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    No partitions.
                  </p>
                )}
              </CardContent>
            </Card>
          )
        })}
      </div>
      {change && (
        <StorageChange
          fingerprint={snapshot.fingerprint}
          partition={change.partition}
          action={change.action}
          onClose={() => setChange(null)}
        />
      )}
    </section>
  )
}

const actionText: Record<
  string,
  { title: string; verb: string; done: string }
> = {
  mount: { title: "Mount", verb: "Mount", done: "mounted" },
  unmount: { title: "Unmount", verb: "Unmount", done: "unmounted" },
  "persistent-mount": {
    title: "Mount at boot",
    verb: "Add to /etc/fstab",
    done: "will mount at boot",
  },
  "persistent-unmount": {
    title: "Stop mounting at boot",
    verb: "Remove from /etc/fstab",
    done: "will no longer mount at boot",
  },
}

function StorageChange({
  fingerprint,
  partition,
  action,
  onClose,
}: {
  fingerprint: string
  partition: StoragePartition
  action: StorageOperation["action"]
  onClose: () => void
}) {
  const client = useQueryClient()
  const persistent = action.startsWith("persistent-")
  const [target, setTarget] = React.useState(
    partition.mountPoints[0]?.target ??
      `/mnt/${partition.label || partition.name || "data"}`
  )
  const [filesystem, setFilesystem] = React.useState(partition.filesystem ?? "")
  const text = actionText[action]
  const apply = useMutation({
    mutationFn: () =>
      api<StorageState>("/storage", {
        method: "POST",
        body: JSON.stringify({
          action,
          device: partition.path,
          target: action === "mount" ? undefined : target || undefined,
          filesystem:
            action === "persistent-mount" ? filesystem || undefined : undefined,
          expectedFingerprint: fingerprint,
          confirmation: persistent
            ? "CONFIRM PERSISTENT MOUNT"
            : "CONFIRM STORAGE CHANGE",
        } satisfies StorageOperation),
      }),
    onSuccess: (state) => {
      void client.invalidateQueries({ queryKey: ["storage"] })
      toast.add({
        title: `${partition.path} ${text.done}`,
        description: state.warning,
        type: "success",
      })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form
          className="flex flex-col gap-5"
          onSubmit={(event) => {
            event.preventDefault()
            apply.mutate()
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {text.title} {partition.path}
            </DialogTitle>
            <DialogDescription>
              {action === "mount" &&
                "The filesystem is mounted for the current boot under /run/media."}
              {action === "unmount" &&
                "Programs using files on this filesystem may lose access to them."}
              {persistent &&
                "Tako edits only entries it owns in /etc/fstab and checks that the inventory has not changed."}
            </DialogDescription>
          </DialogHeader>
          {persistent && (
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="storage-target">Mount point</FieldLabel>
                <Input
                  id="storage-target"
                  className="font-mono"
                  required
                  value={target}
                  onChange={(event) => setTarget(event.target.value)}
                />
                <FieldDescription>
                  System directories such as / and /usr are refused.
                </FieldDescription>
              </Field>
              {action === "persistent-mount" && (
                <Field>
                  <FieldLabel htmlFor="storage-filesystem">
                    Filesystem type
                  </FieldLabel>
                  <Input
                    id="storage-filesystem"
                    className="font-mono"
                    required
                    value={filesystem}
                    onChange={(event) => setFilesystem(event.target.value)}
                  />
                </Field>
              )}
            </FieldGroup>
          )}
          {apply.isError && (
            <Alert variant="destructive">
              <AlertTitle>Nothing changed</AlertTitle>
              <AlertDescription>{apply.error.message}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant={action === "unmount" ? "destructive" : "default"}
              disabled={apply.isPending}
            >
              {apply.isPending && <Spinner data-icon="inline-start" />}
              {text.verb}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export const Route = createFileRoute("/storage")({ component: StoragePage })
