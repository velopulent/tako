import { useQuery } from "@tanstack/react-query"
import { ChevronDown, History } from "lucide-react"
import * as React from "react"

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
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { api, type UpdateHistoryEntry } from "@/lib/api"
import { formatAbsolute, formatRelative } from "@/lib/update-advisories"

const maxShownEntries = 10
const collapsedEntries = 4
const mergeWindowMs = 10 * 60 * 1000

// Native manager histories can contain adjacent duplicate transactions.
function mergeHistory(entries: UpdateHistoryEntry[]): UpdateHistoryEntry[] {
  const merged: UpdateHistoryEntry[] = []
  for (const entry of entries.slice(0, 20)) {
    const previous = merged[merged.length - 1]
    if (
      previous &&
      Math.abs(entry.time - previous.time) <= mergeWindowMs &&
      samePackages(entry.packages, previous.packages)
    ) {
      continue
    }
    merged.push(entry)
    if (merged.length === maxShownEntries) break
  }
  return merged
}

function samePackages(
  left: Record<string, string>,
  right: Record<string, string>
) {
  const leftNames = Object.keys(left).sort()
  const rightNames = Object.keys(right).sort()
  return (
    leftNames.length === rightNames.length &&
    leftNames.every((name, index) => name === rightNames[index])
  )
}

export function UpdateHistoryCard({ enabled }: { enabled: boolean }) {
  const [showAll, setShowAll] = React.useState(false)
  const query = useQuery({
    queryKey: ["update-history"],
    queryFn: () =>
      api<{ items: UpdateHistoryEntry[]; available: boolean }>(
        "/updates/history"
      ),
    enabled,
    staleTime: 5 * 60 * 1000,
  })

  // Parent hides history when its provider is unavailable.
  if (!enabled || !query.data?.available) return null

  const history = mergeHistory(query.data.items ?? [])
  if (history.length === 0) return null
  const shown = showAll ? history : history.slice(0, collapsedEntries)

  return (
    <Card id="update-history" size="sm">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <History
            className="size-4 text-muted-foreground"
            aria-hidden="true"
          />
          Recent transactions
        </CardTitle>
        <CardDescription>As recorded by the package manager.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-1">
        <ol className="flex flex-col">
          {shown.map((entry) => {
            const names = Object.keys(entry.packages).sort()
            return (
              <li key={entry.time}>
                <Collapsible>
                  <CollapsibleTrigger className="group/entry flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left outline-none transition-colors hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring">
                    <span className="flex min-w-0 flex-1 flex-col">
                      <time
                        dateTime={new Date(entry.time).toISOString()}
                        title={formatAbsolute(entry.time)}
                        className="text-sm"
                      >
                        {formatRelative(entry.time)}
                      </time>
                      <span className="truncate text-xs text-muted-foreground">
                        {names.slice(0, 3).join(", ")}
                        {names.length > 3 ? ` +${names.length - 3}` : ""}
                      </span>
                    </span>
                    <Badge variant="secondary" className="tabular-nums">
                      {names.length}
                    </Badge>
                    <ChevronDown
                      className="size-4 shrink-0 text-muted-foreground transition-transform duration-200 group-data-[panel-open]/entry:rotate-180 motion-reduce:transition-none"
                      aria-hidden="true"
                    />
                  </CollapsibleTrigger>
                  <CollapsibleContent>
                    <ul className="mx-2 mb-2 flex max-h-56 flex-col overflow-y-auto overscroll-contain rounded-md border bg-muted/30 text-xs">
                      {names.map((name) => (
                        <li
                          key={name}
                          className="flex items-center justify-between gap-3 px-2.5 py-1.5 not-last:border-b"
                        >
                          <span className="min-w-0 truncate">{name}</span>
                          <span className="shrink-0 truncate font-mono text-muted-foreground">
                            {entry.packages[name]}
                          </span>
                        </li>
                      ))}
                    </ul>
                  </CollapsibleContent>
                </Collapsible>
              </li>
            )
          })}
        </ol>
        {history.length > collapsedEntries && (
          <Button
            variant="ghost"
            size="sm"
            className="self-start"
            onClick={() => setShowAll((value) => !value)}
          >
            {showAll
              ? "Show fewer"
              : `Show ${history.length - collapsedEntries} more`}
          </Button>
        )}
      </CardContent>
    </Card>
  )
}
