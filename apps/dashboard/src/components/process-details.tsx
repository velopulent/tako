import { useQuery } from "@tanstack/react-query"

import {
  api,
  type ProcessDetails as ProcessDetailsData,
  type ProcessInfo,
} from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { ProcessSignal } from "@/components/process-signal"

const bytes = (value: number) => {
  const units = ["B", "KiB", "MiB", "GiB"]
  let amount = value
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index += 1
  }
  return `${amount.toFixed(index ? 1 : 0)} ${units[index]}`
}

function ProcessLink({ process }: { process: ProcessInfo }) {
  return (
    <span>
      {process.program} (PID {process.pid}, start {process.started})
    </span>
  )
}

export function ProcessDetails({
  process,
  onClose,
}: {
  process: ProcessInfo | null
  onClose: () => void
}) {
  const query = useQuery({
    queryKey: ["process-details", process?.pid, process?.started],
    enabled: process !== null,
    queryFn: () =>
      api<ProcessDetailsData>(
        `/processes/${process?.pid}?started=${process?.started}`
      ),
  })
  const details = query.data
  return (
    <Sheet open={process !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>{process?.program ?? "Process details"}</SheetTitle>
          <SheetDescription>
            PID {process?.pid ?? "-"} · start identity {process?.started ?? "-"}
          </SheetDescription>
        </SheetHeader>
        {query.isPending && <Skeleton className="m-4 h-48" />}
        {query.isError && (
          <Alert className="m-4" variant="destructive">
            <AlertTitle>Process details unavailable</AlertTitle>
            <AlertDescription>{query.error.message}</AlertDescription>
          </Alert>
        )}
        {details && (
          <div className="space-y-4 p-4">
            {(details.process.permissionDenied ||
              details.accessIssues?.length) && (
              <Alert>
                <AlertTitle>Some process data is restricted</AlertTitle>
                <AlertDescription>
                  {details.process.reason ?? details.accessIssues?.join(" ")}
                </AlertDescription>
              </Alert>
            )}
            <Card>
              <CardHeader>
                <CardTitle>Relationships</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <div>
                  <span className="text-muted-foreground">Parent: </span>
                  {details.parent ? (
                    <ProcessLink process={details.parent} />
                  ) : (
                    "None"
                  )}
                </div>
                <div>
                  <span className="text-muted-foreground">Children: </span>
                  {(details.children ?? []).length ? (
                    <ul className="mt-1 list-disc pl-5">
                      {(details.children ?? []).map((child) => (
                        <li key={`${child.pid}:${child.started}`}>
                          <ProcessLink process={child} />
                        </li>
                      ))}
                    </ul>
                  ) : (
                    "None"
                  )}
                </div>
              </CardContent>
            </Card>
            <ProcessSignal process={details.process} />
            <Card>
              <CardHeader>
                <CardTitle>Resource history</CardTitle>
              </CardHeader>
              <CardContent>
                {(details.history ?? []).length ? (
                  <div className="max-h-48 overflow-auto text-xs">
                    {(details.history ?? []).map((sample) => (
                      <div
                        className="grid grid-cols-4 gap-2 border-b py-1 last:border-0"
                        key={sample.timestamp}
                      >
                        <span>
                          {new Date(sample.timestamp).toLocaleTimeString()}
                        </span>
                        <span>CPU {sample.cpuTime.toFixed(1)}s</span>
                        <span>RAM {bytes(sample.memory)}</span>
                        <span>Read {bytes(sample.diskRead)}</span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    History starts after the next inventory refresh.
                  </p>
                )}
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Cgroup</CardTitle>
              </CardHeader>
              <CardContent>
                <pre className="max-h-32 overflow-auto rounded bg-muted p-3 text-xs whitespace-pre-wrap">
                  {details.cgroup || "Unavailable"}
                </pre>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Open files ({(details.openFiles ?? []).length})</CardTitle>
              </CardHeader>
              <CardContent>
                {(details.openFiles ?? []).length ? (
                  <ul className="max-h-48 space-y-1 overflow-auto font-mono text-xs">
                    {(details.openFiles ?? []).map((file, index) => (
                      <li key={`${file}:${index}`}>{file}</li>
                    ))}
                  </ul>
                ) : (
                  <Empty>
                    <EmptyHeader>
                      <EmptyTitle>No readable open files</EmptyTitle>
                      <EmptyDescription>
                        Permission restrictions or a short-lived process may
                        hide this data.
                      </EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                )}
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Sockets ({(details.sockets ?? []).length})</CardTitle>
              </CardHeader>
              <CardContent>
                {(details.sockets ?? []).length ? (
                  <div className="space-y-1 text-xs">
                    {(details.sockets ?? []).map((socket, index) => (
                      <div
                        className="flex flex-wrap gap-2"
                        key={`${socket.protocol}:${socket.local}:${index}`}
                      >
                        <Badge variant="outline">{socket.protocol}</Badge>
                        <span className="font-mono">{socket.local}</span>
                        {socket.remote && <span>→ {socket.remote}</span>}
                        {socket.state && (
                          <span className="text-muted-foreground">
                            {socket.state}
                          </span>
                        )}
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-sm text-muted-foreground">
                    No readable sockets.
                  </p>
                )}
              </CardContent>
            </Card>
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
