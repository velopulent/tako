import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import * as React from "react"
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
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  api,
  type FirewallSnapshot,
  type FirewallState,
  type SessionResponse,
} from "@/lib/api"

export function FirewallControls() {
  const client = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const status = useQuery({
    queryKey: ["firewall"],
    queryFn: () => api<FirewallSnapshot>("/firewall"),
  })
  const [port, setPort] = React.useState("")
  const [confirmation, setConfirmation] = React.useState("")
  const apply = useMutation({
    mutationFn: () =>
      api<FirewallState>("/firewall", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify({
          backend: status.data?.backend ?? "auto",
          action: "add-port",
          port,
          expectedFingerprint: status.data?.fingerprint,
          confirmation,
          persist: true,
        }),
      }),
    onSuccess: () => void client.invalidateQueries({ queryKey: ["firewall"] }),
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>Firewall</CardTitle>
        <CardDescription>
          Runtime and persistent rules are shown separately; changes preserve a
          fresh fingerprint and explicit access confirmation.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {status.isPending && (
          <div className="h-16 animate-pulse rounded bg-muted" />
        )}
        {status.isError && (
          <Alert variant="destructive">
            <AlertTitle>Firewall unavailable</AlertTitle>
            <AlertDescription>
              Gain Administrative access and verify firewalld or UFW is active.
            </AlertDescription>
          </Alert>
        )}
        {status.data && (
          <>
            <div className="flex flex-wrap gap-2">
              <Badge variant={status.data.active ? "secondary" : "outline"}>
                {status.data.backend || "none"} ·{" "}
                {status.data.active ? "active" : "inactive"}
              </Badge>
              {status.data.conflicted && (
                <Badge variant="destructive">conflicted</Badge>
              )}
            </div>
            <pre className="max-h-44 overflow-auto rounded bg-muted p-3 text-xs">
              {status.data.rules.join("\n") || "No rules reported."}
            </pre>
          </>
        )}
        <FieldGroup className="grid gap-4 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="firewall-port">Port/protocol</FieldLabel>
            <Input
              id="firewall-port"
              value={port}
              onChange={(event) => setPort(event.target.value)}
              placeholder="443/tcp"
            />
            <FieldDescription>
              Fixed grammar only: PORT/tcp or PORT/udp.
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="firewall-confirmation">
              Confirmation
            </FieldLabel>
            <Input
              id="firewall-confirmation"
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              placeholder="CONFIRM FIREWALL CHANGE"
            />
          </Field>
        </FieldGroup>
        <Button
          onClick={() => apply.mutate()}
          disabled={!status.data || !port || apply.isPending}
        >
          Allow port
        </Button>
        {apply.isError && (
          <Alert variant="destructive">
            <AlertTitle>Rule rejected</AlertTitle>
            <AlertDescription>
              The firewall owner, fingerprint, or access confirmation was not
              acceptable.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}
