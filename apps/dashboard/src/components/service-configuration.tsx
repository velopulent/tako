import { useQuery } from "@tanstack/react-query"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type UnitConfiguration } from "@/lib/api"

export function ServiceConfiguration({
  scope,
  unit,
}: {
  scope: string
  unit: string
}) {
  const configuration = useQuery({
    queryKey: ["service-configuration", scope, unit],
    queryFn: () =>
      api<UnitConfiguration>(
        `/services/${scope}/${encodeURIComponent(unit)}/configuration`
      ),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Unit configuration</CardTitle>
        <CardDescription>
          Read-only content from the unit path reported by systemd.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {configuration.isPending && <Skeleton className="h-48 w-full" />}
        {configuration.isError && (
          <Alert variant="destructive">
            <AlertTitle>Configuration unavailable</AlertTitle>
            <AlertDescription>
              {configuration.error.message ||
                "The unit file could not be read under the current UNIX authority."}
            </AlertDescription>
          </Alert>
        )}
        {configuration.data && (
          <div className="space-y-2">
            <p className="font-mono text-xs text-muted-foreground">
              {configuration.data.path}
            </p>
            {configuration.data.truncated && (
              <p className="text-sm text-muted-foreground">
                The unit file is larger than the 1 MiB preview limit.
              </p>
            )}
            <pre className="max-h-[32rem] overflow-auto rounded-lg border bg-muted/30 p-3 text-xs whitespace-pre-wrap">
              {configuration.data.content}
            </pre>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
