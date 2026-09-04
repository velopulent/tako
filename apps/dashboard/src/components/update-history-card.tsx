import { useQuery } from "@tanstack/react-query"
import { ChevronRight, History } from "lucide-react"
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

const maxShownEntries = 3
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

function formatTime(ms: number) {
  return new Date(ms).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  })
}

export function UpdateHistoryCard({ enabled }: { enabled: boolean }) {
  const [expanded, setExpanded] = React.useState<number | null>(null)
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

  return (
    <Card id="update-history">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-lg">
          <History className="size-4 text-muted-foreground" />
          Update history
        </CardTitle>
        <CardDescription>
          Recent package-update transactions reported by the package manager.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {history.map((entry) => {
          const names = Object.keys(entry.packages).sort()
          const isOpen = expanded === entry.time
          return (
            <Collapsible
              key={entry.time}
              open={isOpen}
              onOpenChange={(open) => setExpanded(open ? entry.time : null)}
            >
              <div className="flex items-center justify-between gap-2 rounded-md border px-3 py-2">
                <div className="flex min-w-0 items-center gap-2">
                  <CollapsibleTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={isOpen ? "Collapse" : "Expand"}
                      />
                    }
                  >
                    <ChevronRight
                      className={
                        isOpen
                          ? "rotate-90 transition-transform"
                          : "transition-transform"
                      }
                    />
                  </CollapsibleTrigger>
                  <span className="truncate text-sm">
                    {formatTime(entry.time)}
                  </span>
                </div>
                <Badge variant="secondary">
                  {names.length} package{names.length === 1 ? "" : "s"}
                </Badge>
              </div>
              <CollapsibleContent>
                <ul className="flow-list mt-1 flex flex-wrap gap-x-4 gap-y-1 rounded-md border bg-muted/20 px-3 py-2 text-xs">
                  {names.map((name) => (
                    <li key={name} title={`${name} ${entry.packages[name]}`}>
                      {name}
                    </li>
                  ))}
                </ul>
              </CollapsibleContent>
            </Collapsible>
          )
        })}
      </CardContent>
    </Card>
  )
}
