import { useMutation } from "@tanstack/react-query"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type ServiceImpact } from "@/lib/api"
import {
  type ServiceActionName,
  serviceActionNames,
} from "@/lib/service-actions"

type ServiceActionsProps = {
  scope: string
  unit: string
  csrfToken: string
  administrative: boolean
  pending: boolean
  onAction: (name: ServiceActionName) => void
}

function ServiceAction({
  scope,
  unit,
  name,
  csrfToken,
  authorityReady,
  authorityMessage,
  pending,
  onAction,
}: {
  scope: string
  unit: string
  name: ServiceActionName
  csrfToken: string
  authorityReady: boolean
  authorityMessage: string
  pending: boolean
  onAction: (name: ServiceActionName) => void
}) {
  const preview = useMutation<ServiceImpact, Error>({
    mutationFn: () =>
      api<ServiceImpact>(
        `/services/${scope}/${encodeURIComponent(unit)}/actions/preview`,
        {
          method: "POST",
          headers: { "X-CSRF-Token": csrfToken },
          body: JSON.stringify({ action: name }),
        }
      ),
  })

  return (
    <AlertDialog>
      <AlertDialogTrigger
        onClick={() => preview.mutate()}
        render={
          <Button
            variant={
              name === "stop" || name === "mask" ? "destructive" : "outline"
            }
            size="sm"
            disabled={!authorityReady || pending}
          />
        }
      >
        {name[0].toUpperCase() + name.slice(1)}
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {name} {unit}?
          </AlertDialogTitle>
          <AlertDialogDescription>
            {authorityMessage} This changes {scope} systemd state.
          </AlertDialogDescription>
        </AlertDialogHeader>
        {preview.isPending && <Skeleton className="h-20 w-full" />}
        {preview.isError && (
          <Alert variant="destructive">
            <AlertTitle>Impact preview unavailable</AlertTitle>
            <AlertDescription>
              {preview.error.message || "Refresh the service and try again."}
            </AlertDescription>
          </Alert>
        )}
        {preview.data && (
          <div className="space-y-3 text-sm">
            <p>
              Current state: <strong>{preview.data.currentState}</strong>
              {preview.data.currentSubState
                ? ` / ${preview.data.currentSubState}`
                : ""}
            </p>
            {preview.data.affected.length > 0 ? (
              <div>
                <p className="font-medium">Related units</p>
                <ul className="mt-1 max-h-32 list-disc space-y-1 overflow-auto pl-5 text-muted-foreground">
                  {preview.data.affected.slice(0, 32).map((item) => (
                    <li key={`${item.relationship}-${item.name}`}>
                      {item.name} ({item.relationship})
                    </li>
                  ))}
                </ul>
              </div>
            ) : (
              <p className="text-muted-foreground">
                No related units were reported.
              </p>
            )}
            {preview.data.warnings.map((warning) => (
              <p key={warning} className="text-muted-foreground">
                {warning}
              </p>
            ))}
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            disabled={
              pending || preview.isPending || preview.isError || !preview.data
            }
            onClick={() => onAction(name)}
          >
            Confirm
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

export function ServiceActions({
  scope,
  unit,
  csrfToken,
  administrative,
  pending,
  onAction,
}: ServiceActionsProps) {
  const needsAdministrative = scope === "system"
  const sessionReady = csrfToken !== ""
  const authorityReady =
    sessionReady && (!needsAdministrative || administrative)
  const authorityMessage = !sessionReady
    ? "Session authorization is still loading."
    : needsAdministrative
      ? "Gain Administrative access before changing a system unit."
      : "The action runs as your authenticated UNIX user."

  return (
    <div className="flex flex-wrap gap-2">
      {serviceActionNames.map((name) => (
        <ServiceAction
          key={name}
          scope={scope}
          unit={unit}
          name={name}
          csrfToken={csrfToken}
          authorityReady={authorityReady}
          authorityMessage={authorityMessage}
          pending={pending}
          onAction={onAction}
        />
      ))}
    </div>
  )
}
