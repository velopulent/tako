import { useInfiniteQuery } from "@tanstack/react-query"
import { getRouteApi, useNavigate } from "@tanstack/react-router"
import { useVirtualizer } from "@tanstack/react-virtual"
import {
  ArrowUpIcon,
  BookmarkIcon,
  DownloadIcon,
  FilterIcon,
  PauseIcon,
  PlayIcon,
  SearchIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"
import * as React from "react"

import { logPriority, PriorityBadge } from "@/components/log-list"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
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
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { api, type JournalPage, type LogEntry, streamURL } from "@/lib/api"
import { formatDateTime, formatTimestamp } from "@/lib/format"
import { QueryError } from "@/lib/page"
import type { LogsSearch } from "@/lib/search"
import { cn } from "@/lib/utils"

const priorityItems = [
  { value: "all", label: "All priorities" },
  { value: "0..3", label: "Errors and worse" },
  { value: "0..4", label: "Warnings and worse" },
  { value: "0..5", label: "Notices and worse" },
  { value: "0..6", label: "Info and worse" },
]

const rangeItems = [
  { value: "boot", label: "Current boot" },
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "all", label: "All time" },
]

const rangeMilliseconds: Record<string, number> = {
  "1h": 3_600_000,
  "24h": 86_400_000,
  "7d": 604_800_000,
}

type Filters = Pick<
  LogsSearch,
  "priority" | "range" | "unit" | "executable" | "text" | "since" | "until"
>

const savedKey = "tako-log-views:v2"
type SavedView = { id: string; name: string; filters: Filters }

function readSaved(): SavedView[] {
  try {
    const parsed = JSON.parse(
      localStorage.getItem(savedKey) ?? "[]"
    ) as SavedView[]
    return Array.isArray(parsed)
      ? parsed.filter((view) => view?.id && view?.name)
      : []
  } catch {
    return []
  }
}

function writeSaved(views: SavedView[]) {
  try {
    localStorage.setItem(savedKey, JSON.stringify(views))
  } catch {
    // Saved views are a convenience; ignore storage failures.
  }
}

// queryParams builds the journal query for the current filters. Relative
// ranges are resolved when the query runs, so "last hour" stays current.
function queryParams(filters: Filters, extra: Record<string, string> = {}) {
  const params = new URLSearchParams(extra)
  const range = filters.range ?? "boot"
  if (filters.since || filters.until) {
    if (filters.since) params.set("since", filters.since)
    if (filters.until) params.set("until", filters.until)
  } else if (range === "boot") {
    params.set("boot", "current")
  } else if (rangeMilliseconds[range]) {
    params.set(
      "since",
      new Date(Date.now() - rangeMilliseconds[range]).toISOString()
    )
  }
  if (filters.priority) params.set("priority", filters.priority)
  if (filters.unit) params.set("unit", filters.unit)
  if (filters.executable) params.set("executable", filters.executable)
  if (filters.text) params.set("text", filters.text)
  return params
}

const entryKey = (entry: LogEntry) =>
  entry.id ?? `${entry.timestamp}:${entry.unit}:${entry.message}`

const rowHeight = 36
const maxLive = 1000

export function JournalBrowser() {
  const search = getRouteApi("/logs").useSearch()
  const navigate = useNavigate({ from: "/logs" })
  const filters: Filters = {
    priority: search.priority,
    range: search.range,
    unit: search.unit,
    executable: search.executable,
    text: search.text,
    since: search.since,
    until: search.until,
  }
  const setFilters = (next: Partial<Filters>) =>
    navigate({
      search: (previous) => ({ ...previous, ...next }),
      replace: true,
    })
  const filterKey = JSON.stringify(filters)

  const [following, setFollowing] = React.useState(true)
  const [live, setLive] = React.useState<LogEntry[]>([])
  const [waiting, setWaiting] = React.useState(0)
  const [selected, setSelected] = React.useState<LogEntry | null>(null)
  const viewport = React.useRef<HTMLDivElement>(null)
  const atTop = React.useRef(true)

  const query = useInfiniteQuery({
    queryKey: ["journal", filterKey],
    initialPageParam: "",
    queryFn: ({ pageParam, signal }) =>
      api<JournalPage>(
        `/logs?${queryParams(filters, { limit: "200", details: "true", ...(pageParam ? { cursor: pageParam } : {}) })}`,
        { signal }
      ),
    getNextPageParam: (page) => page.nextCursor || undefined,
  })

  // Reset live entries whenever the filters change.
  // biome-ignore lint/correctness/useExhaustiveDependencies: filterKey is the dependency that matters
  React.useEffect(() => {
    setLive([])
    setWaiting(0)
  }, [filterKey])

  // Live tail: new entries arrive at the top. While the reader is scrolled
  // into history they are held back and counted instead.
  // biome-ignore lint/correctness/useExhaustiveDependencies: filterKey captures the filters used by queryParams
  React.useEffect(() => {
    if (!following || filters.until) return
    const params = queryParams(
      { ...filters, since: undefined, until: undefined, range: "all" },
      { details: "true" }
    )
    const source = new EventSource(streamURL(`/api/v1/logs/stream?${params}`))
    let pending: LogEntry[] = []
    source.addEventListener("log", (event) => {
      try {
        pending.push(
          JSON.parse((event as MessageEvent<string>).data) as LogEntry
        )
      } catch {
        // Ignore malformed events.
      }
    })
    const flush = window.setInterval(() => {
      if (!pending.length) return
      const batch = pending.reverse()
      pending = []
      if (!atTop.current) {
        setWaiting((count) => count + batch.length)
      }
      setLive((current) => [...batch, ...current].slice(0, maxLive))
    }, 250)
    return () => {
      window.clearInterval(flush)
      source.close()
    }
  }, [following, filterKey])

  const items = React.useMemo(() => {
    const seen = new Set<string>()
    const merged: LogEntry[] = []
    for (const entry of [
      ...live,
      ...(query.data?.pages.flatMap((page) => page.items) ?? []),
    ]) {
      const key = entryKey(entry)
      if (seen.has(key)) continue
      seen.add(key)
      merged.push(entry)
    }
    return merged
  }, [live, query.data])

  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => viewport.current,
    estimateSize: () => rowHeight,
    overscan: 20,
  })

  const scrollToTop = () => {
    viewport.current?.scrollTo({ top: 0, behavior: "smooth" })
    setWaiting(0)
  }

  const activeFilters = [
    filters.unit && `Unit: ${filters.unit}`,
    filters.executable && `Executable: ${filters.executable}`,
    (filters.since || filters.until) && "Custom time range",
  ].filter(Boolean) as string[]

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <SearchField
          value={filters.text ?? ""}
          onChange={(text) => setFilters({ text: text || undefined })}
        />
        <Select
          items={rangeItems}
          value={
            filters.since || filters.until
              ? "custom"
              : (filters.range ?? "boot")
          }
          onValueChange={(value) =>
            setFilters({
              range:
                value === "boot"
                  ? undefined
                  : (String(value) as Filters["range"]),
              since: undefined,
              until: undefined,
            })
          }
        >
          <SelectTrigger size="sm" aria-label="Time range" className="w-40">
            <SelectValue placeholder="Custom range" />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {rangeItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <Select
          items={priorityItems}
          value={filters.priority ?? "all"}
          onValueChange={(value) =>
            setFilters({
              priority: value === "all" ? undefined : String(value),
            })
          }
        >
          <SelectTrigger size="sm" aria-label="Priority" className="w-48">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {priorityItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <MoreFilters filters={filters} onApply={setFilters} />
        <div className="ml-auto flex items-center gap-2">
          <Button
            variant={following ? "secondary" : "outline"}
            size="sm"
            onClick={() => setFollowing((value) => !value)}
            aria-pressed={following}
          >
            {following ? (
              <PauseIcon data-icon="inline-start" />
            ) : (
              <PlayIcon data-icon="inline-start" />
            )}
            {following ? "Live" : "Paused"}
          </Button>
          <SavedViews
            filters={filters}
            onApply={(saved) =>
              navigate({ search: { ...saved }, replace: true })
            }
          />
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="outline" size="sm" />}
            >
              <DownloadIcon data-icon="inline-start" />
              Export
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuGroup>
                <DropdownMenuLabel>
                  Up to 500 matching entries
                </DropdownMenuLabel>
                <DropdownMenuItem
                  render={
                    <a
                      href={`/api/v1/logs/export?${queryParams(filters, { format: "csv" })}`}
                      download
                    />
                  }
                >
                  CSV
                </DropdownMenuItem>
                <DropdownMenuItem
                  render={
                    <a
                      href={`/api/v1/logs/export?${queryParams(filters, { format: "json", details: "true" })}`}
                      download
                    />
                  }
                >
                  JSON
                </DropdownMenuItem>
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {activeFilters.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          {activeFilters.map((label) => (
            <Badge key={label} variant="secondary" className="font-normal">
              {label}
            </Badge>
          ))}
          <Button
            variant="ghost"
            size="sm"
            onClick={() =>
              setFilters({
                unit: undefined,
                executable: undefined,
                since: undefined,
                until: undefined,
              })
            }
          >
            <XIcon data-icon="inline-start" />
            Clear
          </Button>
        </div>
      )}

      {query.isPending ? (
        <Skeleton className="h-[60vh]" />
      ) : query.isError ? (
        <QueryError error={query.error} retry={() => void query.refetch()} />
      ) : items.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyTitle>No journal entries</EmptyTitle>
            <EmptyDescription>
              Nothing matches these filters. Try a wider time range or a lower
              priority.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
          {waiting > 0 && (
            <Button
              size="sm"
              className="absolute top-2 left-1/2 z-10 -translate-x-1/2 shadow-md"
              onClick={scrollToTop}
            >
              <ArrowUpIcon data-icon="inline-start" />
              {waiting} new {waiting === 1 ? "entry" : "entries"}
            </Button>
          )}
          <div
            ref={viewport}
            className="h-[calc(100svh-15rem)] min-h-80 overflow-auto"
            onScroll={(event) => {
              atTop.current = event.currentTarget.scrollTop < rowHeight
              if (atTop.current) setWaiting(0)
              const remaining =
                event.currentTarget.scrollHeight -
                event.currentTarget.scrollTop -
                event.currentTarget.clientHeight
              if (
                remaining < 600 &&
                query.hasNextPage &&
                !query.isFetchingNextPage
              )
                void query.fetchNextPage()
            }}
          >
            <ul
              style={{
                height: virtualizer.getTotalSize(),
                position: "relative",
              }}
              aria-label="Journal entries"
            >
              {virtualizer.getVirtualItems().map((row) => {
                const entry = items[row.index]
                return (
                  <li
                    key={entryKey(entry)}
                    className="absolute inset-x-0"
                    style={{
                      height: rowHeight,
                      transform: `translateY(${row.start}px)`,
                    }}
                  >
                    <button
                      type="button"
                      onClick={() => setSelected(entry)}
                      className={cn(
                        "flex size-full items-center gap-3 border-b px-3 text-left font-mono text-xs outline-none hover:bg-accent/50 focus-visible:bg-accent",
                        Number(entry.priority) <= 3 &&
                          entry.priority !== "" &&
                          "bg-destructive/5"
                      )}
                    >
                      <time
                        className="w-36 shrink-0 text-muted-foreground tabular-nums"
                        dateTime={entry.timestamp}
                      >
                        {formatTimestamp(entry.timestamp)}
                      </time>
                      <PriorityBadge
                        value={entry.priority}
                        className="hidden shrink-0 sm:inline-flex"
                      />
                      <span className="hidden w-48 shrink-0 truncate font-sans font-medium md:inline">
                        {entry.unit || "kernel"}
                      </span>
                      <span className="min-w-0 flex-1 truncate">
                        {entry.message}
                      </span>
                    </button>
                  </li>
                )
              })}
            </ul>
            <div className="flex justify-center p-3 text-xs text-muted-foreground">
              {query.isFetchingNextPage ? (
                <Spinner />
              ) : query.hasNextPage ? (
                "Scroll for older entries"
              ) : (
                `${items.length.toLocaleString()} entries · start of range`
              )}
            </div>
          </div>
        </div>
      )}

      <Sheet
        open={selected !== null}
        onOpenChange={(open) => !open && setSelected(null)}
      >
        <SheetContent className="w-full gap-0 sm:max-w-xl">
          {selected && (
            <>
              <SheetHeader className="border-b">
                <SheetTitle className="flex items-center gap-2">
                  <PriorityBadge value={selected.priority} />
                  <span className="truncate">{selected.unit || "kernel"}</span>
                </SheetTitle>
                <SheetDescription>
                  {formatDateTime(selected.timestamp)}
                </SheetDescription>
              </SheetHeader>
              <div className="flex flex-col gap-4 overflow-auto p-4">
                <pre className="rounded-lg bg-muted p-3 font-mono text-xs whitespace-pre-wrap break-words">
                  {selected.message}
                </pre>
                <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-xs">
                  <dt className="text-muted-foreground">Priority</dt>
                  <dd>{logPriority(selected.priority).label}</dd>
                  {Object.entries(selected.details ?? {})
                    .filter(([key]) => key !== "MESSAGE" && key !== "PRIORITY")
                    .sort(([a], [b]) => a.localeCompare(b))
                    .map(([key, value]) => (
                      <div key={key} className="contents">
                        <dt className="font-mono text-muted-foreground">
                          {key}
                        </dt>
                        <dd className="min-w-0 font-mono break-all">{value}</dd>
                      </div>
                    ))}
                </dl>
                {selected.unit && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="self-start"
                    onClick={() => {
                      setSelected(null)
                      setFilters({ unit: selected.unit })
                    }}
                  >
                    <FilterIcon data-icon="inline-start" />
                    Show only {selected.unit}
                  </Button>
                )}
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  )
}

// SearchField searches the journal on the server after a short pause.
function SearchField({
  value,
  onChange,
}: {
  value: string
  onChange: (value: string) => void
}) {
  const [draft, setDraft] = React.useState(value)
  React.useEffect(() => setDraft(value), [value])
  React.useEffect(() => {
    if (draft === value) return
    const timer = window.setTimeout(() => onChange(draft.trim()), 400)
    return () => window.clearTimeout(timer)
  }, [draft, value, onChange])
  return (
    <InputGroup className="w-full sm:w-72">
      <InputGroupAddon>
        <SearchIcon />
      </InputGroupAddon>
      <InputGroupInput
        value={draft}
        maxLength={512}
        onChange={(event) => setDraft(event.target.value)}
        placeholder="Search messages"
        aria-label="Search messages"
      />
    </InputGroup>
  )
}

function MoreFilters({
  filters,
  onApply,
}: {
  filters: Filters
  onApply: (next: Partial<Filters>) => void
}) {
  const [open, setOpen] = React.useState(false)
  const [unit, setUnit] = React.useState(filters.unit ?? "")
  const [executable, setExecutable] = React.useState(filters.executable ?? "")
  const [since, setSince] = React.useState(
    filters.since ? toLocalInput(filters.since) : ""
  )
  const [until, setUntil] = React.useState(
    filters.until ? toLocalInput(filters.until) : ""
  )
  const count = [
    filters.unit,
    filters.executable,
    filters.since || filters.until,
  ].filter(Boolean).length
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) {
          setUnit(filters.unit ?? "")
          setExecutable(filters.executable ?? "")
          setSince(filters.since ? toLocalInput(filters.since) : "")
          setUntil(filters.until ? toLocalInput(filters.until) : "")
        }
      }}
    >
      <PopoverTrigger render={<Button variant="outline" size="sm" />}>
        <FilterIcon data-icon="inline-start" />
        Filters
        {count > 0 && <Badge className="ml-1 h-4 min-w-4 px-1">{count}</Badge>}
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80">
        <form
          onSubmit={(event) => {
            event.preventDefault()
            onApply({
              unit: unit.trim() || undefined,
              executable: executable.trim() || undefined,
              since: since ? new Date(since).toISOString() : undefined,
              until: until ? new Date(until).toISOString() : undefined,
            })
            setOpen(false)
          }}
        >
          <FieldGroup className="gap-4">
            <Field>
              <FieldLabel htmlFor="log-unit">Unit</FieldLabel>
              <Input
                id="log-unit"
                placeholder="nginx.service"
                maxLength={256}
                value={unit}
                onChange={(event) => setUnit(event.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="log-executable">Executable</FieldLabel>
              <Input
                id="log-executable"
                placeholder="/usr/bin/sshd"
                maxLength={4096}
                value={executable}
                onChange={(event) => setExecutable(event.target.value)}
              />
            </Field>
            <div className="grid grid-cols-2 gap-2">
              <Field>
                <FieldLabel htmlFor="log-since">From</FieldLabel>
                <Input
                  id="log-since"
                  type="datetime-local"
                  value={since}
                  onChange={(event) => setSince(event.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="log-until">To</FieldLabel>
                <Input
                  id="log-until"
                  type="datetime-local"
                  value={until}
                  onChange={(event) => setUntil(event.target.value)}
                />
              </Field>
            </div>
            <FieldDescription>
              A custom time range replaces the range preset.
            </FieldDescription>
            <Button type="submit" size="sm">
              Apply filters
            </Button>
          </FieldGroup>
        </form>
      </PopoverContent>
    </Popover>
  )
}

function toLocalInput(value: string) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ""
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16)
}

function SavedViews({
  filters,
  onApply,
}: {
  filters: Filters
  onApply: (filters: Filters) => void
}) {
  const [views, setViews] = React.useState(readSaved)
  const [naming, setNaming] = React.useState(false)
  const [name, setName] = React.useState("")
  const persist = (next: SavedView[]) => {
    setViews(next)
    writeSaved(next)
  }
  return (
    <Popover onOpenChange={(open) => !open && setNaming(false)}>
      <PopoverTrigger
        render={<Button variant="outline" size="sm" aria-label="Saved views" />}
      >
        <BookmarkIcon data-icon="inline-start" />
        <span className="hidden lg:inline">Views</span>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72 gap-2 p-2">
        <p className="px-2 pt-1 text-xs font-medium text-muted-foreground">
          Saved in this browser
        </p>
        {views.length === 0 && (
          <p className="px-2 py-1 text-sm text-muted-foreground">
            No saved views yet.
          </p>
        )}
        <ul className="flex flex-col">
          {views.map((view) => (
            <li key={view.id} className="group flex items-center gap-1">
              <Button
                variant="ghost"
                size="sm"
                className="flex-1 justify-start truncate"
                onClick={() => onApply(view.filters)}
              >
                {view.name}
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`Delete ${view.name}`}
                onClick={() =>
                  persist(views.filter((item) => item.id !== view.id))
                }
              >
                <Trash2Icon />
              </Button>
            </li>
          ))}
        </ul>
        <Separator className="my-1" />
        {naming ? (
          <form
            className="flex gap-1"
            onSubmit={(event) => {
              event.preventDefault()
              const trimmed = name.trim()
              if (!trimmed) return
              persist([
                ...views.filter((view) => view.name !== trimmed),
                { id: crypto.randomUUID(), name: trimmed, filters },
              ])
              setName("")
              setNaming(false)
            }}
          >
            <Input
              autoFocus
              aria-label="View name"
              placeholder="View name"
              maxLength={64}
              value={name}
              onChange={(event) => setName(event.target.value)}
              className="h-8"
            />
            <Button type="submit" size="sm">
              Save
            </Button>
          </form>
        ) : (
          <Button
            variant="ghost"
            size="sm"
            className="justify-start"
            onClick={() => setNaming(true)}
          >
            Save current filters…
          </Button>
        )}
      </PopoverContent>
    </Popover>
  )
}
