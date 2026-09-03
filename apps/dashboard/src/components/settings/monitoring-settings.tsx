import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { RefreshSelect } from "@/components/refresh-select"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Skeleton } from "@/components/ui/skeleton"
import type { RefreshInterval } from "@/lib/monitoring"

export function MonitoringSettings({
  value,
  onChange,
  error,
  loadError,
  pending,
}: {
  value: RefreshInterval
  onChange: (value: RefreshInterval) => void
  error: boolean
  loadError: boolean
  pending: boolean
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Monitoring</CardTitle>
        <CardDescription>
          Default refresh interval stored in this browser. Metric history is
          retained in memory for 24 hours.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error ? (
          <Alert variant="destructive">
            <AlertTitle>
              {loadError
                ? "Could not load monitoring preference"
                : "Could not save monitoring preference"}
            </AlertTitle>
            <AlertDescription>
              {loadError
                ? "Try reloading this page before changing the setting."
                : "The previous local value remains active."}
            </AlertDescription>
          </Alert>
        ) : null}
        {pending ? (
          <Skeleton className="h-8 w-40" />
        ) : (
          <FieldGroup>
            <Field orientation="responsive">
              <FieldLabel>Default refresh interval</FieldLabel>
              <FieldDescription>
                Used unless this browser has a page-specific override.
              </FieldDescription>
              <RefreshSelect value={value} onChange={onChange} />
            </Field>
          </FieldGroup>
        )}
      </CardContent>
    </Card>
  )
}
