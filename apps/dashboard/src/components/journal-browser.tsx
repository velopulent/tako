import * as React from "react"
import { useQuery } from "@tanstack/react-query"
import { PauseIcon, PlayIcon } from "lucide-react"
import type { ColumnDef } from "@tanstack/react-table"

import { api, type JournalPage, type LogEntry } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
import { Skeleton } from "@/components/ui/skeleton"

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

export function JournalBrowser() {
  const [priority, setPriority] = React.useState("")
  const [unit, setUnit] = React.useState("")
  const [executable, setExecutable] = React.useState("")
  const [text, setText] = React.useState("")
  const [activeUnit, setActiveUnit] = React.useState("")
  const [activeExecutable, setActiveExecutable] = React.useState("")
  const [activeText, setActiveText] = React.useState("")
  const [cursor, setCursor] = React.useState("")
  const [following, setFollowing] = React.useState(true)
  const [live, setLive] = React.useState<LogEntry[]>([])
  const query = useQuery({
    queryKey: [
      "logs",
      priority,
      activeUnit,
      activeExecutable,
      activeText,
      cursor,
    ],
    queryFn: () => {
      const params = new URLSearchParams({ limit: "200" })
      if (priority) params.set("priority", priority)
      if (activeUnit) params.set("unit", activeUnit)
      if (activeExecutable) params.set("executable", activeExecutable)
      if (activeText) params.set("text", activeText)
      if (cursor) params.set("cursor", cursor)
      return api<JournalPage>(`/logs?${params.toString()}`)
    },
  })
  React.useEffect(() => {
    const timer = window.setTimeout(() => {
      setActiveUnit(unit)
      setActiveExecutable(executable)
      setActiveText(text)
      setCursor("")
    }, 250)
    return () => window.clearTimeout(timer)
  }, [unit, executable, text])
  React.useEffect(() => {
    if (!following || typeof EventSource === "undefined") return
    const source = new EventSource("/api/v1/logs/stream")
    source.addEventListener("log", (event) => {
      try {
        setLive((current) =>
          [
            JSON.parse((event as MessageEvent<string>).data) as LogEntry,
            ...current,
          ].slice(0, 500)
        )
      } catch {
        // Ignore malformed stream events; the bounded query remains usable.
      }
    })
    return () => source.close()
  }, [following])
  const items = [...live, ...(query.data?.items ?? [])]
  const resetCursor = () => setCursor("")

  return (
    <div className="space-y-4">
      <FieldGroup className="grid gap-4 @lg/main:grid-cols-2 @4xl/main:grid-cols-4">
        <Field>
          <FieldLabel htmlFor="journal-text">Message contains</FieldLabel>
          <Input
            id="journal-text"
            maxLength={512}
            value={text}
            onChange={(event) => {
              setText(event.target.value)
            }}
            placeholder="failed"
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="journal-unit">Unit</FieldLabel>
          <Input
            id="journal-unit"
            maxLength={256}
            value={unit}
            onChange={(event) => {
              setUnit(event.target.value)
            }}
            placeholder="worker.service"
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="journal-executable">Executable</FieldLabel>
          <Input
            id="journal-executable"
            maxLength={4096}
            value={executable}
            onChange={(event) => {
              setExecutable(event.target.value)
            }}
            placeholder="/usr/bin/worker"
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="journal-priority">Priority</FieldLabel>
          <Select
            value={priority || "all"}
            onValueChange={(value) => {
              setPriority(value === "all" ? "" : (value ?? ""))
              resetCursor()
            }}
          >
            <SelectTrigger id="journal-priority" aria-label="Journal priority">
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
              {query.data?.nextCursor && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setLive([])
                    setCursor(query.data?.nextCursor ?? "")
                  }}
                >
                  Older entries
                </Button>
              )}
            </div>
          }
        />
      )}
    </div>
  )
}
