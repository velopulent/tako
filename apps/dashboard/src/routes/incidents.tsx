import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type IncidentTimeline, type SessionResponse } from "@/lib/api"

export function IncidentsPage() {
  const client = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const incidents = useQuery({
    queryKey: ["incidents"],
    queryFn: () => api<IncidentTimeline>("/incidents"),
  })
  const transition = useMutation({
    mutationFn: (id: string) =>
      api(`/notifications/${encodeURIComponent(id)}/acknowledged`, {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
      }),
    onSuccess: () => void client.invalidateQueries({ queryKey: ["incidents"] }),
  })
  return (
    <main className="@container/main flex flex-1 flex-col gap-6 p-4 lg:p-6">
      <div>
        <p className="text-sm text-muted-foreground">
          Correlated journal evidence and built-in notifications, without
          copying raw journal history.
        </p>
      </div>
      {incidents.isPending && <Skeleton className="h-72" />}
      {incidents.isError && (
        <Alert variant="destructive">
          <AlertTitle>Incidents unavailable</AlertTitle>
          <AlertDescription>
            The bounded journal correlation could not be completed.
          </AlertDescription>
        </Alert>
      )}
      {incidents.data && (
        <Card>
          <CardHeader>
            <CardTitle>Incident timeline</CardTitle>
            <CardDescription>
              {incidents.data.partial
                ? "Showing a bounded page; open System logs for older evidence."
                : "Recent service, network, memory, and security signals."}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {incidents.data.items.length === 0 && (
              <p className="text-sm text-muted-foreground">
                No incidents in the selected window.
              </p>
            )}
            {incidents.data.items.map((item) => (
              <div
                key={item.id}
                className="flex flex-col gap-2 rounded-lg border p-3 sm:flex-row sm:items-start"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap gap-2">
                    <Badge
                      variant={
                        item.severity === "critical" ? "destructive" : "outline"
                      }
                    >
                      {item.severity}
                    </Badge>
                    <Badge variant="outline">{item.kind}</Badge>
                    <span className="text-xs text-muted-foreground">
                      {new Date(item.timestamp).toLocaleString()}
                    </span>
                  </div>
                  <p className="mt-2 text-sm">{item.summary}</p>
                  <p className="text-xs text-muted-foreground">
                    {item.source || "journal"}
                  </p>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => transition.mutate(item.id)}
                  disabled={transition.isPending}
                >
                  Acknowledge
                </Button>
              </div>
            ))}
          </CardContent>
        </Card>
      )}
    </main>
  )
}

export const Route = createFileRoute("/incidents")({
  component: IncidentsPage,
})
