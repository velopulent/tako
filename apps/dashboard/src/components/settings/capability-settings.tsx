import type { Capability } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"

export function CapabilitySettings({
  capabilities,
  pending,
  error,
}: {
  capabilities: Capability[]
  pending: boolean
  error: boolean
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Host capabilities</CardTitle>
        <CardDescription>
          Optional integrations detected at runtime.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {pending ? (
          <Skeleton className="h-20 sm:col-span-2 lg:col-span-3" />
        ) : null}
        {error ? (
          <Alert variant="destructive" className="sm:col-span-2 lg:col-span-3">
            <AlertTitle>Could not inspect host capabilities</AlertTitle>
            <AlertDescription>Try reloading this page.</AlertDescription>
          </Alert>
        ) : null}
        {!pending && !error && capabilities.length === 0 ? (
          <Empty className="sm:col-span-2 lg:col-span-3">
            <EmptyHeader>
              <EmptyTitle>No optional capabilities detected</EmptyTitle>
              <EmptyDescription>
                Core host administration remains available.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : null}
        {capabilities.map((item) => (
          <div
            key={item.id}
            className="flex items-center justify-between gap-3 rounded-lg border p-3"
          >
            <div>
              <p className="font-medium capitalize">{item.id}</p>
              <p className="text-xs text-muted-foreground">
                {[item.backend, item.version].filter(Boolean).join(" · ") ||
                  "Built in"}
              </p>
              {item.reason ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  {item.reason}
                </p>
              ) : null}
              {item.setupGuidance ? (
                <p className="mt-1 text-xs">{item.setupGuidance}</p>
              ) : null}
              <p className="mt-2 text-xs text-muted-foreground">
                {item.readable ? "Read" : "No read"} ·{" "}
                {item.mutable ? "Write" : "No write"} ·{" "}
                {item.rollback ? "Rollback" : "No rollback"}
              </p>
              <p className="text-xs text-muted-foreground">
                Authority: {item.readAuthority} read · {item.mutationAuthority}{" "}
                write
              </p>
              <p className="text-xs text-muted-foreground">
                Contract: {item.contract}
              </p>
              {item.missingDependency ? (
                <p className="text-xs text-muted-foreground">
                  Missing: {item.missingDependency}
                </p>
              ) : null}
            </div>
            <Badge
              variant={item.state === "ready" ? "secondary" : "outline"}
              className="capitalize"
            >
              {item.state}
            </Badge>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}
