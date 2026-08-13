import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { ArrowDownToLineIcon, EyeIcon, PauseIcon, PlayIcon } from "lucide-react"
import type { ColumnDef } from "@tanstack/react-table"

import { api, type JournalPage, type LogEntry } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
import { DataTable } from "@/components/data-table"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { SavedLogViews } from "@/components/saved-log-views"

const columns: ColumnDef<LogEntry>[] = [
  {
    accessorKey: "timestamp",
    header: "Time",
    cell: ({ row }) => new Date(row.original.timestamp).toLocaleString(),
  },
  {
    accessorKey: "priority",
    header: "Priority",
    cell: ({ row }) => (
      <Badge variant="outline">{row.original.priority || "-"}</Badge>
    ),
  },
  { accessorKey: "unit", header: "Unit" },
  {
    accessorKey: "message",
    header: "Message",
    cell: ({ row }) => (
      <span className="font-mono text-xs whitespace-normal">
        {row.original.message}
      </span>
    ),
  },
]

function makeParams(filters: {
  priority: string
  boot: string
  since: string
  until: string
  unit: string
  executable: string
  text: string
  details: boolean
  cursor?: string
  limit?: number
}) {
  const params = new URLSearchParams({
    limit: String(filters.limit ?? 200),
  })
  if (filters.priority) params.set("priority", filters.priority)
  if (filters.boot) params.set("boot", filters.boot)
  if (filters.since) params.set("since", filters.since)
  if (filters.until) params.set("until", filters.until)
  if (filters.unit) params.set("unit", filters.unit)
  if (filters.executable) params.set("executable", filters.executable)
  if (filters.text) params.set("text", filters.text)
  if (filters.details) params.set("details", "true")
  if (filters.cursor) params.set("cursor", filters.cursor)
  return params
}

export function JournalBrowser() {
  const [priority, setPriority] = React.useState("")
  const [boot, setBoot] = React.useState("")
  const [since, setSince] = React.useState("")
  const [until, setUntil] = React.useState("")
  const [unit, setUnit] = React.useState("")
  const [executable, setExecutable] = React.useState("")
  const [text, setText] = React.useState("")
  const [activeUnit, setActiveUnit] = React.useState("")
  const [activeBoot, setActiveBoot] = React.useState("")
  const [activeSince, setActiveSince] = React.useState("")
  const [activeUntil, setActiveUntil] = React.useState("")
  const [activeExecutable, setActiveExecutable] = React.useState("")
  const [activeText, setActiveText] = React.useState("")
  const [details, setDetails] = React.useState(false)
  const [cursor, setCursor] = React.useState("")
  const [following, setFollowing] = React.useState(true)
  const [atLatest, setAtLatest] = React.useState(true)
  const [pendingLive, setPendingLive] = React.useState(0)
  const [live, setLive] = React.useState<LogEntry[]>([])
  const [selected, setSelected] = React.useState<LogEntry | null>(null)
  const atLatestRef = React.useRef(true)

  const setLatest = (value: boolean) => {
    atLatestRef.current = value
    setAtLatest(value)
  }
  const filterValues = {
    priority,
    boot: activeBoot,
    since: activeSince,
    until: activeUntil,
    unit: activeUnit,
    executable: activeExecutable,
    text: activeText,
    details,
  }
  const query = useQuery({
    queryKey: ["logs", filterValues, cursor],
    queryFn: () =>
      api<JournalPage>(
        `/logs?${makeParams({ ...filterValues, cursor }).toString()}`
      ),
  })
  const streamParams = React.useMemo(
    () => makeParams(filterValues).toString(),
    [
      priority,
      activeBoot,
      activeSince,
      activeUntil,
      activeUnit,
      activeExecutable,
      activeText,
      details,
    ]
  )
  const exportParams = React.useMemo(
    () => makeParams({ ...filterValues, limit: 500 }).toString(),
    [
      priority,
      activeBoot,
      activeSince,
      activeUntil,
      activeUnit,
      activeExecutable,
      activeText,
      details,
    ]
  )

  React.useEffect(() => {
    const timer = window.setTimeout(() => {
      setActiveUnit(unit)
      setActiveBoot(boot)
      setActiveSince(since)
      setActiveUntil(until)
      setActiveExecutable(executable)
      setActiveText(text)
      setCursor("")
      setLive([])
      setPendingLive(0)
      setLatest(true)
    }, 250)
    return () => window.clearTimeout(timer)
  }, [boot, since, until, unit, executable, text])

  React.useEffect(() => {
    if (!following || typeof EventSource === "undefined") return
    const source = new EventSource(`/api/v1/logs/stream?${streamParams}`)
    source.addEventListener("log", (event) => {
      try {
        const entry = JSON.parse(
          (event as MessageEvent<string>).data
        ) as LogEntry
        if (!atLatestRef.current) {
          setPendingLive((current) => Math.min(current + 1, 500))
          return
        }
        setLive((current) => [entry, ...current].slice(0, 500))
      } catch {
        // Ignore malformed stream events; the bounded query remains usable.
      }
    })
    return () => source.close()
  }, [following, streamParams])

  const items = atLatest
    ? [...live, ...(query.data?.items ?? [])]
    : (query.data?.items ?? [])
  const jumpToLatest = () => {
    setLive([])
    setPendingLive(0)
    setCursor("")
    setLatest(true)
    setFollowing(true)
  }
  const applySavedFilter = (saved: {
    boot?: string
    since?: string
    until?: string
    priority?: string
    unit?: string
    executable?: string
    text?: string
    details?: boolean
  }) => {
    setPriority(saved.priority ?? "")
    setBoot(saved.boot ?? "")
    setSince(saved.since ?? "")
    setUntil(saved.until ?? "")
    setUnit(saved.unit ?? "")
    setExecutable(saved.executable ?? "")
    setText(saved.text ?? "")
    setDetails(Boolean(saved.details))
    setCursor("")
    setLive([])
    setPendingLive(0)
    setLatest(true)
  }

  return (
    <>
      <div className="space-y-4">
        <FieldGroup className="grid gap-4 @lg/main:grid-cols-2 @4xl/main:grid-cols-4">
          <Field>
            <FieldLabel htmlFor="journal-text">Message contains</FieldLabel>
            <Input
              id="journal-text"
              maxLength={512}
              value={text}
              onChange={(event) => setText(event.target.value)}
              placeholder="failed"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="journal-boot">Boot</FieldLabel>
            <Input
              id="journal-boot"
              maxLength={32}
              value={boot}
              onChange={(event) => setBoot(event.target.value)}
              placeholder="current or boot ID"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="journal-since">Since (RFC3339)</FieldLabel>
            <Input
              id="journal-since"
              maxLength={64}
              value={since}
              onChange={(event) => setSince(event.target.value)}
              placeholder="2026-08-13T00:00:00Z"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="journal-until">Until (RFC3339)</FieldLabel>
            <Input
              id="journal-until"
              maxLength={64}
              value={until}
              onChange={(event) => setUntil(event.target.value)}
              placeholder="2026-08-13T23:59:59Z"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="journal-unit">Unit</FieldLabel>
            <Input
              id="journal-unit"
              maxLength={256}
              value={unit}
              onChange={(event) => setUnit(event.target.value)}
              placeholder="worker.service"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="journal-executable">Executable</FieldLabel>
            <Input
              id="journal-executable"
              maxLength={4096}
              value={executable}
              onChange={(event) => setExecutable(event.target.value)}
              placeholder="/usr/bin/worker"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="journal-priority">Priority</FieldLabel>
            <Select
              value={priority || "all"}
              onValueChange={(value) => {
                setPriority(value === "all" ? "" : (value ?? ""))
                setCursor("")
                setLive([])
                setPendingLive(0)
                setLatest(true)
              }}
            >
              <SelectTrigger
                id="journal-priority"
                aria-label="Journal priority"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All priorities</SelectItem>
                <SelectItem value="0..3">Emergency–Error</SelectItem>
                <SelectItem value="4">Warning</SelectItem>
                <SelectItem value="5..7">Notice–Debug</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        </FieldGroup>
        <SavedLogViews filter={filterValues} onApply={applySavedFilter} />
        {pendingLive > 0 && (
          <Button variant="secondary" size="sm" onClick={jumpToLatest}>
            <ArrowDownToLineIcon data-icon="inline-start" />
            {pendingLive} new {pendingLive === 1 ? "entry" : "entries"} — jump
            to latest
          </Button>
        )}
        {query.isPending && <Skeleton className="h-72" />}
        {query.isError && (
          <Alert variant="destructive">
            <AlertTitle>Journal unavailable</AlertTitle>
            <AlertDescription>
              {query.error.message || "The journal query failed."}
            </AlertDescription>
          </Alert>
        )}
        {!query.isPending && !query.isError && !items.length && (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No journal entries</EmptyTitle>
              <EmptyDescription>
                Try a broader filter or time range.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {!query.isPending && !query.isError && items.length > 0 && (
          <DataTable
            data={items}
            columns={columns}
            searchPlaceholder="Search loaded entries"
            onRowClick={setSelected}
            toolbar={
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setFollowing((value) => !value)}
                >
                  {following ? (
                    <PauseIcon data-icon="inline-start" />
                  ) : (
                    <PlayIcon data-icon="inline-start" />
                  )}
                  {following ? "Pause" : "Follow"}
                </Button>
                <Button
                  variant={details ? "secondary" : "outline"}
                  size="sm"
                  onClick={() => {
                    setDetails((value) => !value)
                    setCursor("")
                    setLive([])
                    setPendingLive(0)
                    setLatest(true)
                  }}
                >
                  <EyeIcon data-icon="inline-start" />
                  {details ? "Details on" : "Details off"}
                </Button>
                <a
                  className={buttonVariants({ variant: "outline", size: "sm" })}
                  href={`/api/v1/logs/export?${exportParams}`}
                  download
                >
                  <ArrowDownToLineIcon data-icon="inline-start" />
                  Export CSV
                </a>
                {query.data?.nextCursor && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setLive([])
                      setPendingLive(0)
                      setCursor(query.data?.nextCursor ?? "")
                      setLatest(false)
                    }}
                  >
                    Older entries
                  </Button>
                )}
                {(!atLatest || pendingLive > 0) && (
                  <Button variant="outline" size="sm" onClick={jumpToLatest}>
                    Jump to latest
                  </Button>
                )}
              </div>
            }
          />
        )}
      </div>
      <Sheet
        open={selected !== null}
        onOpenChange={(open) => !open && setSelected(null)}
      >
        <SheetContent className="w-full sm:max-w-xl">
          <SheetHeader>
            <SheetTitle>Journal entry details</SheetTitle>
            <SheetDescription>
              Structured fields from the authenticated journal record.
            </SheetDescription>
          </SheetHeader>
          {selected && (
            <pre className="m-4 max-h-[calc(100vh-10rem)] overflow-auto rounded-lg bg-muted p-4 text-xs whitespace-pre-wrap">
              {JSON.stringify(
                { ...selected, details: selected.details ?? {} },
                null,
                2
              )}
            </pre>
          )}
        </SheetContent>
      </Sheet>
    </>
  )
}
