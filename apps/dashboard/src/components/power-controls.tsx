import { useMutation } from "@tanstack/react-query"
import { AlertTriangleIcon, PowerIcon, RotateCcwIcon } from "lucide-react"
import * as React from "react"

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
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { api, type PowerStatus } from "@/lib/api"

function stateLabel(state: PowerStatus["reboot"]["state"]) {
  return state[0].toUpperCase() + state.slice(1)
}

function PowerAction({
  action,
  status,
  fingerprint,
  csrfToken,
  disabled,
}: {
  action: "reboot" | "shutdown"
  status: PowerStatus["reboot"]
  fingerprint: string
  csrfToken: string
  disabled: boolean
}) {
  const [confirmation, setConfirmation] = React.useState("")
  const request = useMutation({
    mutationFn: () =>
      api<{ action: string; message: string }>("/host/power", {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify({
          action,
          confirmation,
          expectedFingerprint: fingerprint,
        }),
      }),
  })
  const expected = action.toUpperCase()
  return (
    <AlertDialog onOpenChange={(open) => !open && setConfirmation("")}>
      <AlertDialogTrigger
        render={
          <Button
            variant={action === "shutdown" ? "destructive" : "outline"}
            disabled={disabled || !status.available}
          />
        }
      >
        {action === "shutdown" ? (
          <PowerIcon data-icon="inline-start" />
        ) : (
          <RotateCcwIcon data-icon="inline-start" />
        )}
        {action === "shutdown" ? "Shut down" : "Reboot"}
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {action === "shutdown" ? "Shut down" : "Reboot"} this host?
          </AlertDialogTitle>
          <AlertDialogDescription>
            This can disconnect every operator and stop running workloads. Type{" "}
            <strong>{expected}</strong> to confirm.
          </AlertDialogDescription>
        </AlertDialogHeader>
        {status.reason && (
          <Alert variant="destructive">
            <AlertTriangleIcon />
            <AlertTitle>{stateLabel(status.state)}</AlertTitle>
            <AlertDescription>{status.reason}</AlertDescription>
          </Alert>
        )}
        <Field>
          <FieldLabel htmlFor={`${action}-confirmation`}>
            Confirmation
          </FieldLabel>
          <Input
            id={`${action}-confirmation`}
            value={confirmation}
            autoComplete="off"
            onChange={(event) => setConfirmation(event.target.value)}
          />
          <FieldDescription>
            This exact confirmation prevents an accidental power action.
          </FieldDescription>
        </Field>
        {request.isError && (
          <p className="text-sm text-destructive">{request.error.message}</p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            disabled={confirmation !== expected || request.isPending}
            onClick={() => request.mutate()}
          >
            {request.isPending ? "Requesting…" : `Confirm ${action}`}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

export function PowerControls({
  status,
  csrfToken,
  administrative,
}: {
  status: PowerStatus
  csrfToken: string
  administrative: boolean
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle>Power controls</CardTitle>
            <CardDescription>
              Capability-aware, audited actions with explicit typed
              confirmation.
            </CardDescription>
          </div>
          <Badge variant={administrative ? "secondary" : "outline"}>
            {administrative ? "Administrative" : "Read only"}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {!status.available && (
          <Alert variant="destructive">
            <AlertTitle>Power management unavailable</AlertTitle>
            <AlertDescription>
              {status.reason || "The system logind service is unavailable."}
            </AlertDescription>
          </Alert>
        )}
        <div className="grid gap-3 text-sm sm:grid-cols-2">
          {["reboot", "shutdown"].map((action) => {
            const value = status[action as "reboot" | "shutdown"]
            return (
              <div key={action} className="rounded-lg border p-3">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium">
                    {action === "reboot" ? "Reboot" : "Shutdown"}
                  </span>
                  <Badge variant={value.available ? "secondary" : "outline"}>
                    {stateLabel(value.state)}
                  </Badge>
                </div>
                <p className="mt-2 text-muted-foreground">
                  {value.reason ||
                    (value.available
                      ? "Allowed by current host policy."
                      : "Not available on this host.")}
                </p>
              </div>
            )
          })}
        </div>
        {status.inhibitors.length > 0 && (
          <Alert>
            <AlertTitle>Active inhibitors</AlertTitle>
            <AlertDescription>
              <ul className="list-disc space-y-1 pl-4">
                {status.inhibitors.slice(0, 8).map((inhibitor) => (
                  <li key={`${inhibitor.pid}-${inhibitor.what}`}>
                    {inhibitor.who || "Unknown process"}:{" "}
                    {inhibitor.why || inhibitor.what}
                  </li>
                ))}
              </ul>
            </AlertDescription>
          </Alert>
        )}
        <div className="flex flex-wrap gap-2">
          <PowerAction
            action="reboot"
            status={status.reboot}
            fingerprint={status.fingerprint}
            csrfToken={csrfToken}
            disabled={!administrative || !status.available}
          />
          <PowerAction
            action="shutdown"
            status={status.shutdown}
            fingerprint={status.fingerprint}
            csrfToken={csrfToken}
            disabled={!administrative || !status.available}
          />
        </div>
      </CardContent>
    </Card>
  )
}
