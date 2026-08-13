import { useQuery } from "@tanstack/react-query"

import { api, type UpdateStatus } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"

function bytes(value: number) {
  if (!value) return "-"
  const units = ["B", "KiB", "MiB", "GiB"]
  let amount = value
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index++
  }
  return `${amount.toFixed(index ? 1 : 0)} ${units[index]}`
}

export function UpdateInventory() {
  const query = useQuery({
    queryKey: ["updates"],
    queryFn: () => api<UpdateStatus>("/updates"),
  })

  if (query.isPending) return <Skeleton className="h-72" />
  if (query.isError) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Update inventory unavailable</AlertTitle>
        <AlertDescription>{query.error.message}</AlertDescription>
      </Alert>
    )
  }
  if (!query.data) return null
  const status = query.data
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <CardTitle>{status.backend || "Software updates"}</CardTitle>
            <CardDescription>{status.message}</CardDescription>
          </div>
          <Badge variant={status.available ? "secondary" : "outline"}>
            {status.available ? "Read-only inventory" : "Unavailable"}
          </Badge>
        </div>
        {status.version && (
          <p className="text-xs text-muted-foreground">
            Backend {status.version} · contract {status.contract}
          </p>
        )}
        {status.externalLock && (
          <Alert variant="destructive">
            <AlertTitle>Another package tool holds a lock</AlertTitle>
            <AlertDescription>
              {status.lockReason ||
                "Update inventory may be incomplete until the other operation finishes."}
            </AlertDescription>
          </Alert>
        )}
        {!status.available && status.reason && (
          <Alert>
            <AlertTitle>Read-only updates are unavailable</AlertTitle>
            <AlertDescription>{status.reason}</AlertDescription>
          </Alert>
        )}
      </CardHeader>
      <CardContent>
        {status.packages.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No installed-software updates</EmptyTitle>
              <EmptyDescription>
                The selected backend reported no available updates.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full min-w-[48rem] text-sm">
              <thead className="bg-muted/50 text-left">
                <tr>
                  <th className="px-3 py-2 font-medium">Package</th>
                  <th className="px-3 py-2 font-medium">Installed</th>
                  <th className="px-3 py-2 font-medium">Available</th>
                  <th className="px-3 py-2 font-medium">Severity</th>
                  <th className="px-3 py-2 font-medium">Size</th>
                  <th className="px-3 py-2 font-medium">Summary</th>
                </tr>
              </thead>
              <tbody>
                {status.packages.map((item) => (
                  <tr
                    key={`${item.name}-${item.architecture ?? ""}`}
                    className="border-t"
                  >
                    <td className="px-3 py-2 font-medium">
                      {item.name}
                      {item.architecture && ` (${item.architecture})`}
                    </td>
                    <td className="px-3 py-2">{item.currentVersion || "-"}</td>
                    <td className="px-3 py-2">{item.candidateVersion}</td>
                    <td className="px-3 py-2">{item.severity || "-"}</td>
                    <td className="px-3 py-2">{bytes(item.size ?? 0)}</td>
                    <td className="max-w-sm px-3 py-2">
                      {item.summary || item.details || "-"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
