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
import { Checkbox } from "@/components/ui/checkbox"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
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

export type FirewallRuleGroup = {
  zone: string
  rules: string[]
}

export function groupFirewallRules(
  rules: readonly string[]
): FirewallRuleGroup[] {
  const groups = new Map<string, string[]>()

  for (const rule of rules) {
    const trimmed = rule.trim()
    if (!trimmed) continue
    const match = /^(\S+)\s+(.+)$/.exec(trimmed)
    const detail = match?.[2] ?? ""
    const isZonedRule =
      Boolean(match) && (detail.includes(":") || detail.startsWith("rule "))
    const zone = isZonedRule ? (match?.[1] ?? "Unzoned rules") : "Unzoned rules"
    const value = isZonedRule ? detail : trimmed
    const existing = groups.get(zone) ?? []
    existing.push(value)
    groups.set(zone, existing)
  }

  return Array.from(groups, ([zone, groupedRules]) => ({
    zone,
    rules: groupedRules,
  }))
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
      <CardContent className="flex flex-col gap-4">
        {status.isPending && <Skeleton className="h-16 w-full rounded-lg" />}
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
              {status.data.defaultZone && (
                <Badge variant="outline">
                  Runtime default: {status.data.defaultZone}
                </Badge>
              )}
              {status.data.persistentDefaultZone && (
                <Badge variant="outline">
                  Persistent default: {status.data.persistentDefaultZone}
                </Badge>
              )}
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              <RuleList
                title="Runtime policy"
                rules={status.data.runtimeRules ?? status.data.rules}
                zones={status.data.zones}
                defaultZone={status.data.defaultZone}
              />
              <RuleList
                title="Persistent policy"
                rules={status.data.persistentRules ?? []}
                zones={status.data.zones}
                defaultZone={status.data.persistentDefaultZone}
              />
            </div>
          </>
        )}
        {rollbackState?.rollbackRequired && (
          <Alert>
            <AlertTitle>Rollback guard active</AlertTitle>
            <AlertDescription className="flex flex-col gap-2">
              <span>{rollbackState.warning}</span>
              <span className="font-mono text-xs">
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
            <Select
              items={Object.entries(actionLabels).map(([value, label]) => ({
                value: value as FirewallMutation,
                label,
              }))}
              value={action}
              onValueChange={(next) => {
                if (!next) return
                setAction(next as FirewallMutation)
                setConfirmation("")
              }}
            >
              <SelectTrigger id="firewall-action" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {Object.entries(actionLabels).map(([value, label]) => (
                    <SelectItem key={value} value={value}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
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
          <Field orientation="horizontal" className="md:col-span-2">
            <Checkbox
              id="firewall-persist"
              checked={persist}
              onCheckedChange={(checked) => setPersist(checked === true)}
            />
            <FieldLabel htmlFor="firewall-persist" className="font-normal">
              Persist firewalld rule changes
            </FieldLabel>
          </Field>
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

function RuleList({
  title,
  rules,
  zones,
  defaultZone,
}: {
  title: string
  rules: string[]
  zones: string[]
  defaultZone?: string
}) {
  const groupedRules = groupFirewallRules(rules)
  const knownZones = zones
    .filter((zone) => !groupedRules.some((group) => group.zone === zone))
    .map((zone) => ({ zone, rules: [] }))
  const groups = [...groupedRules, ...knownZones]

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{title}</CardTitle>
        <CardDescription>
          {groups.length > 0
            ? "Rules grouped by reported zone."
            : "No zone or rule data reported."}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex max-h-80 flex-col gap-4 overflow-auto">
        {groups.map((group) => (
          <section key={group.zone} className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <Badge
                variant={group.zone === defaultZone ? "secondary" : "outline"}
              >
                {group.zone}
              </Badge>
              {group.zone === defaultZone && (
                <span className="text-xs text-muted-foreground">default</span>
              )}
            </div>
            {group.rules.length > 0 ? (
              <ul className="flex flex-col gap-2">
                {group.rules.map((rule, index) => (
                  <li
                    key={[group.zone, rule, index].join(":")}
                    className="whitespace-pre-wrap break-words font-mono text-xs text-muted-foreground"
                  >
                    {rule}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-xs text-muted-foreground">
                No rules reported.
              </p>
            )}
          </section>
        ))}
        {groups.length === 0 && (
          <p className="text-xs text-muted-foreground">No rules reported.</p>
        )}
      </CardContent>
    </Card>
  )
}
