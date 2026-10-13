import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import {
  CheckIcon,
  CircleCheckIcon,
  CpuIcon,
  HardDriveIcon,
  NetworkIcon,
  RotateCcwIcon,
  ServerCrashIcon,
  ShieldAlertIcon,
  SquareActivityIcon,
  TriangleAlertIcon,
} from "lucide-react"
import * as React from "react"

import { useAdminAccess } from "@/components/admin-access"
import { StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { api, type Incident, type IncidentTimeline } from "@/lib/api"
import { formatDateTime, formatRelative } from "@/lib/format"
import { Page, QueryError } from "@/lib/page"
import { cn } from "@/lib/utils"

const ranges = { "24h": 24, "3d": 72, "7d": 168 } as const
type Range = keyof typeof ranges

const kinds: Record<Incident["kind"], { label: string; icon: typeof CpuIcon }> =
  {
    memory: { label: "Memory", icon: CpuIcon },
    crash: { label: "Crash", icon: ServerCrashIcon },
    storage: { label: "Storage", icon: HardDriveIcon },
    security: { label: "Security", icon: ShieldAlertIcon },
    network: { label: "Network", icon: NetworkIcon },
    service: { label: "Service", icon: SquareActivityIcon },
    other: { label: "Error", icon: TriangleAlertIcon },
  }

function IncidentsPage() {
  const [range, setRange] = React.useState<Range>("24h")
  const [show, setShow] = React.useState<"open" | "all">("open")
  const query = useQuery({
    queryKey: ["incidents", range],
    queryFn: () =>
      api<IncidentTimeline>(
        `/incidents?since=${encodeURIComponent(new Date(Date.now() - ranges[range] * 3_600_000).toISOString().replace(/\.\d+Z$/, "Z"))}`
      ),
    refetchInterval: 60_000,
  })
  const items = (query.data?.items ?? []).filter(
    (item) => show === "all" || item.state === "open"
  )
  const open =
    query.data?.items.filter((item) => item.state === "open").length ?? 0
  return (
    <Page
      description="Errors from the system journal, grouped when they repeat."
      actions={
        <div className="flex flex-wrap gap-2">
          <ToggleGroup
            value={[show]}
            onValueChange={(value) =>
              value[0] && setShow(value[0] as typeof show)
            }
            variant="outline"
            size="sm"
          >
            <ToggleGroupItem value="open">
              Open{query.data ? ` · ${open}` : ""}
            </ToggleGroupItem>
            <ToggleGroupItem value="all">All</ToggleGroupItem>
          </ToggleGroup>
          <ToggleGroup
            value={[range]}
            onValueChange={(value) => value[0] && setRange(value[0] as Range)}
            variant="outline"
            size="sm"
          >
            {Object.keys(ranges).map((value) => (
              <ToggleGroupItem key={value} value={value}>
                {value}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>
      }
    >
      {query.isPending ? (
        <Skeleton className="h-80" />
      ) : query.isError ? (
        <QueryError error={query.error} retry={() => void query.refetch()} />
      ) : items.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <CircleCheckIcon />
            </EmptyMedia>
            <EmptyTitle>
              {show === "open" && query.data.items.length
                ? "Everything is acknowledged"
                : "No errors"}
            </EmptyTitle>
            <EmptyDescription>
              Nothing at error priority or above was logged in the last {range}.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Card className="gap-0 py-0">
          <ul className="divide-y">
            {items.map((item) => (
              <IncidentRow key={item.id} item={item} range={range} />
            ))}
          </ul>
          {query.data.partial && (
            <p className="border-t px-4 py-3 text-xs text-muted-foreground">
              Only the most recent errors were grouped. Open Logs for older
              entries.
            </p>
          )}
        </Card>
      )}
    </Page>
  )
}

function IncidentRow({ item, range }: { item: Incident; range: Range }) {
  const client = useQueryClient()
  const { administrative } = useAdminAccess()
  const kind = kinds[item.kind] ?? kinds.other
  const acknowledged = item.state === "acknowledged"
  const transition = useMutation({
    mutationFn: (state: Incident["state"]) =>
      api(`/incidents/${encodeURIComponent(item.id)}/${state}`, {
        method: "POST",
      }),
    onSuccess: (_, state) =>
      client.setQueryData<IncidentTimeline>(
        ["incidents", range],
        (timeline) =>
          timeline && {
            ...timeline,
            items: timeline.items.map((entry) =>
              entry.id === item.id ? { ...entry, state } : entry
            ),
          }
      ),
  })
  return (
    <li
      className={cn(
        "flex items-start gap-3 px-4 py-3",
        acknowledged && "opacity-60"
      )}
    >
      <span
        className={cn(
          "mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md",
          item.severity === "critical"
            ? "bg-destructive/10 text-destructive"
            : "bg-warning/10 text-warning"
        )}
      >
        <kind.icon className="size-4" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="text-sm break-words">{item.summary}</p>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          {item.severity === "critical" && (
            <StatusBadge tone="danger">Critical</StatusBadge>
          )}
          <span>{kind.label}</span>
          {item.source && (
            <Link
              to="/logs"
              search={{ unit: item.source }}
              className="font-mono hover:text-foreground hover:underline"
            >
              {item.source}
            </Link>
          )}
          <Tooltip>
            <TooltipTrigger render={<span className="tabular-nums" />}>
              {formatRelative(item.lastSeen)}
            </TooltipTrigger>
            <TooltipContent>
              Last {formatDateTime(item.lastSeen)}
              {item.count > 1 && (
                <>
                  <br />
                  First {formatDateTime(item.firstSeen)}
                </>
              )}
            </TooltipContent>
          </Tooltip>
          {item.count > 1 && <Badge variant="secondary">×{item.count}</Badge>}
        </div>
      </div>
      {administrative && (
        <Button
          variant="ghost"
          size="sm"
          disabled={transition.isPending}
          onClick={() =>
            transition.mutate(acknowledged ? "open" : "acknowledged")
          }
        >
          {acknowledged ? (
            <RotateCcwIcon data-icon="inline-start" />
          ) : (
            <CheckIcon data-icon="inline-start" />
          )}
          <span className="hidden sm:inline">
            {acknowledged ? "Reopen" : "Acknowledge"}
          </span>
        </Button>
      )}
    </li>
  )
}

export const Route = createFileRoute("/incidents")({ component: IncidentsPage })
