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
  type FirewallOperation,
  type FirewallSnapshot,
  type FirewallState,
  type SessionResponse,
} from "@/lib/api"

type FirewallMutation = Exclude<
  FirewallOperation["action"],
  "preview" | "commit" | "rollback"
>

const actionLabels: Record<FirewallMutation, string> = {
  enable: "Enable firewall",
  disable: "Disable firewall",
  "default-zone": "Set default zone",
  "add-service": "Allow service",
  "remove-service": "Remove service",
  "add-port": "Allow port",
  "remove-port": "Remove port",
  "add-source": "Allow source",
  "remove-source": "Remove source",
  reload: "Reload policy",
}

function backendValue(
  snapshot?: FirewallSnapshot
): FirewallOperation["backend"] {
  if (snapshot?.backend === "firewalld" || snapshot?.backend === "UFW") {
    return snapshot.backend
  }
  return "auto"
}

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
  const [action, setAction] = React.useState<FirewallMutation>("add-port")
  const [zone, setZone] = React.useState("")
  const [service, setService] = React.useState("")
  const [port, setPort] = React.useState("")
  const [source, setSource] = React.useState("")
  const [defaultZone, setDefaultZone] = React.useState("")
  const [persist, setPersist] = React.useState(true)
  const [rollbackSeconds, setRollbackSeconds] = React.useState("120")
  const [confirmation, setConfirmation] = React.useState("")
  const [rollbackState, setRollbackState] = React.useState<FirewallState>()
  const selectedRisk = action === "disable" || action.startsWith("remove-")
  const isRule =
    action.includes("service") ||
    action.includes("port") ||
    action.includes("source")
  const operation = React.useCallback(
    (requestedAction: FirewallOperation["action"]): FirewallOperation => {
      const checkpointAction =
        requestedAction === "commit" || requestedAction === "rollback"
      return {
        backend: backendValue(status.data),
        action: requestedAction,
        zone: !checkpointAction && isRule ? zone || undefined : undefined,
        service:
          !checkpointAction && action.includes("service")
            ? service || undefined
            : undefined,
        port:
          !checkpointAction && action.includes("port")
            ? port || undefined
            : undefined,
        source:
          !checkpointAction && action.includes("source")
            ? source || undefined
            : undefined,
        defaultZone:
          !checkpointAction && action === "default-zone"
            ? defaultZone || undefined
            : undefined,
        expectedFingerprint:
          requestedAction === "commit" || requestedAction === "rollback"
            ? undefined
            : status.data?.fingerprint,
        confirmation: confirmation || undefined,
        persist: !checkpointAction && persist,
        rollbackSeconds: checkpointAction
          ? undefined
          : Number(rollbackSeconds) || 0,
        checkpoint: checkpointAction ? rollbackState?.checkpoint : undefined,
        rollbackToken: checkpointAction
          ? rollbackState?.rollbackToken
          : undefined,
      }
    },
    [
      action,
      confirmation,
      defaultZone,
      isRule,
      persist,
      port,
      rollbackSeconds,
      rollbackState,
      service,
      source,
      status.data,
      zone,
    ]
  )
  const preview = useMutation({
    mutationFn: () =>
      api<FirewallState>("/firewall/preview", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation(action)),
      }),
  })
  const apply = useMutation({
    mutationFn: (value: FirewallOperation) =>
      api<FirewallState>("/firewall", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(value),
      }),
    onSuccess: (state) => {
      setRollbackState(state.rollbackRequired ? state : undefined)
      void client.invalidateQueries({ queryKey: ["firewall"] })
    },
  })
  const valueMissing =
    (action.includes("service") && !service) ||
    (action.includes("port") && !port) ||
    (action.includes("source") && !source) ||
    (action === "default-zone" && !defaultZone)
  const blocked =
    !status.data ||
    valueMissing ||
    !confirmation ||
    preview.isPending ||
    apply.isPending

  return (
    <Card>
      <CardHeader>
        <CardTitle>Firewall</CardTitle>
        <CardDescription>
          Runtime and persistent firewalld state stay visible separately. Rule
          changes use fresh fingerprints; access-risk changes can auto-rollback.
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
              <Badge
                variant={status.data.synchronized ? "secondary" : "outline"}
              >
                {status.data.synchronized
                  ? "runtime/persistent aligned"
                  : "runtime/persistent differ"}
              </Badge>
              {status.data.conflicted && (
                <Badge variant="destructive">conflicted</Badge>
              )}
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              <RuleList
                title="Runtime rules"
                rules={status.data.runtimeRules ?? status.data.rules}
              />
              <RuleList
                title="Persistent rules"
                rules={status.data.persistentRules ?? []}
              />
            </div>
          </>
        )}
        {rollbackState?.rollbackRequired && (
          <Alert>
            <AlertTitle>Rollback guard active</AlertTitle>
            <AlertDescription className="space-y-2">
              <span className="block">{rollbackState.warning}</span>
              <span className="block font-mono text-xs">
                Deadline:{" "}
                {rollbackState.rollbackDeadline
                  ? new Date(rollbackState.rollbackDeadline).toLocaleString()
                  : "pending"}
              </span>
              <div className="flex flex-wrap gap-2 pt-1">
                <Button
                  size="sm"
                  onClick={() => apply.mutate(operation("commit"))}
                  disabled={apply.isPending}
                >
                  Commit change
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => apply.mutate(operation("rollback"))}
                  disabled={apply.isPending}
                >
                  Roll back
                </Button>
              </div>
            </AlertDescription>
          </Alert>
        )}
        <FieldGroup className="grid gap-4 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="firewall-action">Action</FieldLabel>
            <select
              id="firewall-action"
              className="h-9 rounded-md border bg-background px-3 text-sm"
              value={action}
              onChange={(event) => {
                setAction(event.target.value as FirewallMutation)
                setConfirmation("")
              }}
            >
              {Object.entries(actionLabels).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </Field>
          {isRule && (
            <Field>
              <FieldLabel htmlFor="firewall-zone">Zone</FieldLabel>
              <Input
                id="firewall-zone"
                value={zone}
                onChange={(event) => setZone(event.target.value)}
                placeholder={status.data?.defaultZone || "public"}
              />
              <FieldDescription>
                Blank uses the active default zone.
              </FieldDescription>
            </Field>
          )}
          {action.includes("service") && (
            <Field>
              <FieldLabel htmlFor="firewall-service">Service</FieldLabel>
              <Input
                id="firewall-service"
                value={service}
                onChange={(event) => setService(event.target.value)}
                placeholder="ssh"
              />
            </Field>
          )}
          {action.includes("port") && (
            <Field>
              <FieldLabel htmlFor="firewall-port">Port/protocol</FieldLabel>
              <Input
                id="firewall-port"
                value={port}
                onChange={(event) => setPort(event.target.value)}
                placeholder="443/tcp"
              />
              <FieldDescription>
                Fixed grammar: PORT/tcp or PORT/udp.
              </FieldDescription>
            </Field>
          )}
          {action.includes("source") && (
            <Field>
              <FieldLabel htmlFor="firewall-source">Source</FieldLabel>
              <Input
                id="firewall-source"
                value={source}
                onChange={(event) => setSource(event.target.value)}
                placeholder="192.0.2.0/24"
              />
            </Field>
          )}
          {action === "default-zone" && (
            <Field>
              <FieldLabel htmlFor="firewall-default-zone">
                Default zone
              </FieldLabel>
              <Input
                id="firewall-default-zone"
                value={defaultZone}
                onChange={(event) => setDefaultZone(event.target.value)}
                placeholder="public"
              />
            </Field>
          )}
          <Field>
            <FieldLabel htmlFor="firewall-confirmation">
              Confirmation
            </FieldLabel>
            <Input
              id="firewall-confirmation"
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              placeholder={
                selectedRisk
                  ? "CONFIRM FIREWALL ACCESS"
                  : "CONFIRM FIREWALL CHANGE"
              }
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="firewall-rollback">
              Rollback seconds
            </FieldLabel>
            <Input
              id="firewall-rollback"
              type="number"
              min={selectedRisk ? 30 : 0}
              max={600}
              value={rollbackSeconds}
              onChange={(event) => setRollbackSeconds(event.target.value)}
            />
            <FieldDescription>
              0 disables the timed guard; access-risk changes require 30–600.
            </FieldDescription>
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={persist}
              onChange={(event) => setPersist(event.target.checked)}
            />
            Persist firewalld rule changes
          </label>
        </FieldGroup>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={blocked}
            onClick={() => preview.mutate()}
          >
            Preview
          </Button>
          <Button
            disabled={blocked}
            onClick={() => apply.mutate(operation(action))}
          >
            {actionLabels[action]}
          </Button>
        </div>
        {preview.data && (
          <Alert>
            <AlertTitle>Preview ready</AlertTitle>
            <AlertDescription>{preview.data.warning}</AlertDescription>
          </Alert>
        )}
        {(preview.isError || apply.isError) && (
          <Alert variant="destructive">
            <AlertTitle>Rule rejected</AlertTitle>
            <AlertDescription>
              Refresh the firewall state and verify owner, fingerprint, access
              confirmation, and rollback settings.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}

function RuleList({ title, rules }: { title: string; rules: string[] }) {
  return (
    <div className="rounded-lg border p-3">
      <div className="mb-2 text-sm font-medium">{title}</div>
      <pre className="max-h-36 overflow-auto whitespace-pre-wrap text-xs text-muted-foreground">
        {rules.join("\n") || "No rules reported."}
      </pre>
    </div>
  )
}
