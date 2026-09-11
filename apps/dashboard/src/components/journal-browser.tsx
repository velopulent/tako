import { useQuery, useQueryClient } from "@tanstack/react-query"
import { getRouteApi, useNavigate } from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import { ArrowDownToLineIcon, EyeIcon, PauseIcon, PlayIcon } from "lucide-react"
import * as React from "react"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { SavedLogViews } from "@/components/saved-log-views"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
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
  SelectGroup,
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
import { api, type JournalPage, type LogEntry } from "@/lib/api"

const journalPriorityItems = [
  { value: "all", label: "All priorities" },
  { value: "0..3", label: "Emergency–Error" },
  { value: "4", label: "Warning" },
  { value: "5..7", label: "Notice–Debug" },
]

const priorities = [
  "Emergency",
  "Alert",
  "Critical",
  "Error",
  "Warning",
  "Notice",
  "Info",
  "Debug",
]
const entryKey = (entry: LogEntry) =>
  entry.id ??
  `${entry.timestamp}:${entry.unit}:${entry.priority}:${entry.message}`
const uniqueEntries = (entries: LogEntry[]) => [
  ...new Map(entries.map((entry) => [entryKey(entry), entry])).values(),
]

const columns: ColumnDef<DataTableFeatures, LogEntry>[] = [
  {
    accessorKey: "timestamp",
    header: "Time",
    cell: ({ row }) => new Date(row.original.timestamp).toLocaleString(),
  },
  {
    accessorKey: "priority",
    header: "Priority",
    cell: ({ row }) => (
      <Badge
        variant={Number(row.original.priority) <= 3 ? "destructive" : "outline"}
      >
        {priorities[Number(row.original.priority)] ?? "Unknown"}
      </Badge>
    ),
  },
  { accessorKey: "unit", header: "Unit" },
  {
    accessorKey: "message",
    header: "Message",
    size: 360,
    meta: { wrap: true },
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
  const search = getRouteApi("/logs").useSearch()
  const navigate = useNavigate({ from: "/logs" })
  const patch = (next: Partial<typeof search>) =>
    navigate({
      search: (previous) => ({ ...previous, ...next }),
      replace: true,
    })
  const committed = {
    boot: search.boot ?? "",
    since: search.since ?? "",
    until: search.until ?? "",
    unit: search.unit ?? "",
    executable: search.executable ?? "",
    text: search.text ?? "",
  }
  const [draft, setDraft] = React.useState(committed)
  const [seen, setSeen] = React.useState(committed)
  if (
    committed.boot !== seen.boot ||
    committed.since !== seen.since ||
    committed.until !== seen.until ||
    committed.unit !== seen.unit ||
    committed.executable !== seen.executable ||
    committed.text !== seen.text
  ) {
    setSeen(committed)
    setDraft(committed)
  }
  const boot = draft.boot
  const since = draft.since
  const until = draft.until
  const unit = draft.unit
  const executable = draft.executable
  const text = draft.text
  const setBoot = (value: string) =>
    setDraft((current) => ({ ...current, boot: value }))
  const setSince = (value: string) =>
    setDraft((current) => ({ ...current, since: value }))
  const setUntil = (value: string) =>
    setDraft((current) => ({ ...current, until: value }))
  const setUnit = (value: string) =>
    setDraft((current) => ({ ...current, unit: value }))
  const setExecutable = (value: string) =>
    setDraft((current) => ({ ...current, executable: value }))
  const setText = (value: string) =>
    setDraft((current) => ({ ...current, text: value }))
  const [advanced, setAdvanced] = React.useState(false)
  const [connection, setConnection] = React.useState("Connecting")
  const [reading, setReading] = React.useState(false)
  const readingRef = React.useRef(false)
  const [details, setDetails] = React.useState(false)
  const [cursor, setCursor] = React.useState("")
  const [following, setFollowing] = React.useState(true)
  const [atLatest, setAtLatest] = React.useState(true)
  const [pendingLive, setPendingLive] = React.useState(0)
  const queryClient = useQueryClient()
  const [scrollVersion, setScrollVersion] = React.useState(0)
  const [live, setLive] = React.useState<LogEntry[]>([])
  const [selected, setSelected] = React.useState<LogEntry | null>(null)
  const atLatestRef = React.useRef(true)

  const setLatest = (value: boolean) => {
    atLatestRef.current = value
    setAtLatest(value)
  }
  const filterValues = {
    priority: search.priority ?? "",
    boot: search.boot ?? "",
    since: search.since ?? "",
    until: search.until ?? "",
    unit: search.unit ?? "",
    executable: search.executable ?? "",
    text: search.text ?? "",
    details,
  }
  const query = useQuery({
    staleTime: 0,
    queryKey: ["logs", filterValues, cursor],
    queryFn: ({ signal }) =>
      api<JournalPage>(
        `/logs?${makeParams({ ...filterValues, cursor }).toString()}`,
        { signal }
      ),
  })
  const streamParams = makeParams(filterValues).toString()
  const exportParams = makeParams({ ...filterValues, limit: 500 }).toString()
  // biome-ignore lint/correctness/useExhaustiveDependencies: setLatest is a render-local helper (ref write + setState); listing it would re-arm the debounce timer on every render
  React.useEffect(() => {
    const timer = window.setTimeout(() => {
      if (
        boot === (search.boot ?? "") &&
        since === (search.since ?? "") &&
        until === (search.until ?? "") &&
        unit === (search.unit ?? "") &&
        executable === (search.executable ?? "") &&
        text === (search.text ?? "")
      ) {
        return
      }
      void navigate({
        search: (previous) => ({
          ...previous,
          boot: boot || undefined,
          since: since || undefined,
          until: until || undefined,
          unit: unit || undefined,
          executable: executable || undefined,
          text: text || undefined,
        }),
        replace: true,
      })
      setCursor("")
      setLive([])
      setPendingLive(0)
      setLatest(true)
    }, 250)
    return () => window.clearTimeout(timer)
  }, [
    boot,
    since,
    until,
    unit,
    executable,
    text,
    search.boot,
    search.since,
    search.until,
    search.unit,
    search.executable,
    search.text,
    navigate,
  ])

  React.useEffect(() => {
    if (!following || typeof EventSource === "undefined") return
    const source = new EventSource(`/api/v1/logs/stream?${streamParams}`)
    let queue: LogEntry[] = []
    source.addEventListener("open", () => setConnection("Live"))
    source.addEventListener("error", () => setConnection("Reconnecting"))
    source.addEventListener("log", (event) => {
      try {
        const entry = JSON.parse(
          (event as MessageEvent<string>).data
        ) as LogEntry
        if (!entry.timestamp || typeof entry.message !== "string") return
        if (!atLatestRef.current || readingRef.current) {
          setPendingLive((current) => Math.min(current + 1, 500))
          return
        }
        queue.push(entry)
        if (queue.length > 500) queue = queue.slice(-500)
      } catch {
        /* Invalid events do not replace the last valid snapshot. */
      }
    })
    const timer = window.setInterval(() => {
      if (!queue.length) return
      if (readingRef.current || !atLatestRef.current) {
        setPendingLive((current) => Math.min(current + queue.length, 500))
        queue = []
        return
      }
      const batch = queue.reverse()
      queue = []
      setLive((current) => uniqueEntries([...batch, ...current]).slice(0, 500))
    }, 200)
    return () => {
      window.clearInterval(timer)
      source.close()
    }
  }, [following, streamParams])

  const items = atLatest
    ? uniqueEntries([...live, ...(query.data?.items ?? [])])
    : (query.data?.items ?? [])
  const jumpToLatest = () => {
    readingRef.current = false
    setReading(false)
    void queryClient.invalidateQueries({ queryKey: ["logs"] })
    setScrollVersion((current) => current + 1)
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
    setBoot(saved.boot ?? "")
    setSince(saved.since ?? "")
    setUntil(saved.until ?? "")
    setUnit(saved.unit ?? "")
    setExecutable(saved.executable ?? "")
    setText(saved.text ?? "")
    setDetails(Boolean(saved.details))
    patch({
      boot: saved.boot || undefined,
      since: saved.since || undefined,
      until: saved.until || undefined,
      unit: saved.unit || undefined,
      executable: saved.executable || undefined,
      text: saved.text || undefined,
      priority: saved.priority || undefined,
    })
    setCursor("")
    setLive([])
    setPendingLive(0)
    setLatest(true)
  }

  return (
    <>
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="outline">
            {following ? connection : "Paused"}
            {reading ? " · reading history" : ""}
          </Badge>
          <Button
            variant="outline"
            onClick={() => setAdvanced(!advanced)}
            aria-expanded={advanced}
          >
            Advanced filters
          </Button>
          <Button
            variant="outline"
            onClick={() => {
              setSince(new Date(Date.now() - 3600000).toISOString())
              setUntil("")
            }}
          >
            Last hour
          </Button>
          <Button
            variant="outline"
            onClick={() => {
              setSince(new Date(Date.now() - 86400000).toISOString())
              setUntil("")
            }}
          >
            Last 24 hours
          </Button>
          <Button
            variant="ghost"
            onClick={() => {
              setBoot("current")
              setSince("")
              setUntil("")
            }}
          >
            Current boot
          </Button>
        </div>
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
          {advanced && (
            <>
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
            </>
          )}
          <Field>
            <FieldLabel htmlFor="journal-priority">Priority</FieldLabel>
            <Select
              items={journalPriorityItems}
              value={search.priority || "all"}
              onValueChange={(value) => {
                patch({
                  priority:
                    value === "all" || !value ? undefined : String(value),
                })
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
                <SelectGroup>
                  {journalPriorityItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
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
            key={scrollVersion}
            onScrollPosition={(top) => {
              const paused = top > 40
              readingRef.current = paused
              setReading(paused)
            }}
            data={items}
            columns={columns}
            searchPlaceholder="Search loaded entries"
            search={search.q}
            onSearchChange={(q) => patch({ q: q || undefined })}
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
