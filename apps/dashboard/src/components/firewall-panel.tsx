import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  PlusIcon,
  ShieldAlertIcon,
  ShieldCheckIcon,
  ShieldOffIcon,
  XIcon,
} from "lucide-react"
import * as React from "react"

import { AdminRequired, useAdminAccess } from "@/components/admin-access"
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
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
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { toast } from "@/components/ui/toast"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  api,
  type FirewallOperation,
  type FirewallSnapshot,
  type FirewallState,
} from "@/lib/api"
import { QueryError } from "@/lib/page"

type RuleKind = "service" | "port" | "source"
type Zone = {
  name: string
  services: string[]
  ports: string[]
  sources: string[]
  interfaces: string[]
  rich: string[]
}

const kindLabels: Record<RuleKind, string> = {
  service: "Services",
  port: "Ports",
  source: "Sources",
}

// zonesOf turns firewalld's "zone key: values" lines into zone records.
export function zonesOf(snapshot: FirewallSnapshot): Zone[] {
  const zones = new Map<string, Zone>()
  const zone = (name: string) => {
    let value = zones.get(name)
    if (!value) {
      value = {
        name,
        services: [],
        ports: [],
        sources: [],
        interfaces: [],
        rich: [],
      }
      zones.set(name, value)
    }
    return value
  }
  for (const name of snapshot.zones) zone(name)
  for (const rule of snapshot.rules) {
    const rich = /^(\S+)\s+(rule\s.+)$/.exec(rule.trim())
    if (rich) {
      zone(rich[1]).rich.push(rich[2])
      continue
    }
    const match = /^(\S+)\s+([a-z -]+):\s*(.*)$/.exec(rule.trim())
    if (!match) continue
    const [, name, key, rest] = match
    const values = rest.split(/\s+/).filter(Boolean)
    const target = zone(name)
    if (key === "services") target.services.push(...values)
    else if (key === "ports") target.ports.push(...values)
    else if (key === "sources") target.sources.push(...values)
    else if (key === "interfaces") target.interfaces.push(...values)
  }
  return [...zones.values()]
}

function backendOf(snapshot?: FirewallSnapshot): FirewallOperation["backend"] {
  return snapshot?.backend === "firewalld" || snapshot?.backend === "UFW"
    ? snapshot.backend
    : "auto"
}

function risky(action: FirewallOperation["action"]) {
  return action === "disable" || action.startsWith("remove-")
}

function useFirewallChange(
  snapshot: FirewallSnapshot | undefined,
  onGuard: (state: FirewallState) => void
) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (
      operation: Omit<
        FirewallOperation,
        "backend" | "expectedFingerprint" | "confirmation"
      >
    ) =>
      api<FirewallState>("/firewall", {
        method: "POST",
        body: JSON.stringify({
          ...operation,
          backend: backendOf(snapshot),
          expectedFingerprint: snapshot?.fingerprint,
          confirmation: risky(operation.action)
            ? "CONFIRM FIREWALL ACCESS"
            : "CONFIRM FIREWALL CHANGE",
          rollbackSeconds: risky(operation.action) ? 120 : undefined,
        } satisfies FirewallOperation),
      }),
    onSuccess: (state) => {
      client.setQueryData(["firewall"], state.snapshot)
      if (state.rollbackRequired) onGuard(state)
      else
        toast.add({
          title: state.warning ?? "Firewall updated",
          type: "success",
        })
    },
    onError: (error) =>
      toast.add({
        title: "Firewall unchanged",
        description: error.message,
        type: "error",
      }),
  })
}

export function FirewallPanel() {
  const { administrative } = useAdminAccess()
  const query = useQuery({
    queryKey: ["firewall"],
    queryFn: () => api<FirewallSnapshot>("/firewall"),
  })
  const [guard, setGuard] = React.useState<FirewallState>()
  const [adding, setAdding] = React.useState<string>()
  const change = useFirewallChange(query.data, setGuard)
  if (query.isPending) return <Skeleton className="h-64" />
  if (query.isError)
    return <QueryError error={query.error} retry={() => void query.refetch()} />
  const snapshot = query.data
  const idle = administrative && !change.isPending && !guard
  const editable = idle && !snapshot.readOnly && snapshot.active
  const switchable =
    idle &&
    !snapshot.conflicted &&
    (snapshot.backend === "firewalld" || snapshot.backend === "UFW")
  const zones =
    snapshot.backend === "firewalld"
      ? zonesOf(snapshot).filter(
          (zone) =>
            zone.interfaces.length ||
            zone.sources.length ||
            zone.name === snapshot.defaultZone
        )
      : []
  return (
    <div className="flex flex-col gap-4">
      {guard && (
        <FirewallGuard state={guard} onDone={() => setGuard(undefined)} />
      )}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            {snapshot.active ? (
              <ShieldCheckIcon className="size-4 text-success" />
            ) : (
              <ShieldOffIcon className="size-4 text-muted-foreground" />
            )}
            Firewall
          </CardTitle>
          <CardDescription>
            {snapshot.active ? "Active" : "Inactive"} ·{" "}
            {[snapshot.backend, snapshot.version].filter(Boolean).join(" ")}
          </CardDescription>
          <CardAction>
            <Switch
              checked={snapshot.active}
              disabled={!switchable}
              aria-label="Firewall enabled"
              onCheckedChange={(on) =>
                change.mutate({ action: on ? "enable" : "disable" })
              }
            />
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {!administrative && (
            <AdminRequired>
              Changing firewall rules requires administrative access.
            </AdminRequired>
          )}
          {snapshot.readOnly &&
            snapshot.reason &&
            !(snapshot.active === false && !snapshot.conflicted) && (
              <Alert>
                <AlertTitle>Read-only</AlertTitle>
                <AlertDescription>{snapshot.reason}</AlertDescription>
              </Alert>
            )}
          {snapshot.conflicted && (
            <Alert variant="destructive">
              <AlertTitle>More than one firewall is active</AlertTitle>
              <AlertDescription>
                Disable either firewalld or UFW before making changes here.
              </AlertDescription>
            </Alert>
          )}
          {snapshot.active && !snapshot.synchronized && (
            <Alert>
              <ShieldAlertIcon />
              <AlertTitle>Running rules differ from saved rules</AlertTitle>
              <AlertDescription>
                A reload replaces the running rules with the saved ones.
              </AlertDescription>
              <AlertAction>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={!editable}
                  onClick={() => change.mutate({ action: "reload" })}
                >
                  Reload
                </Button>
              </AlertAction>
            </Alert>
          )}
          {snapshot.backend === "firewalld" && snapshot.zones.length > 0 && (
            <Field orientation="horizontal" className="max-w-sm">
              <FieldLabel htmlFor="default-zone">Default zone</FieldLabel>
              <Select
                value={snapshot.defaultZone ?? ""}
                disabled={!editable}
                onValueChange={(value) =>
                  value &&
                  value !== snapshot.defaultZone &&
                  change.mutate({ action: "default-zone", defaultZone: value })
                }
              >
                <SelectTrigger id="default-zone" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {snapshot.zones.map((zone) => (
                      <SelectItem key={zone} value={zone}>
                        {zone}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          )}
        </CardContent>
      </Card>
      {zones.map((zone) => (
        <Card key={zone.name} size="sm">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              {zone.name}
              {zone.name === snapshot.defaultZone && (
                <Badge variant="secondary">Default</Badge>
              )}
            </CardTitle>
            <CardDescription>
              {zone.interfaces.length
                ? `Interfaces: ${zone.interfaces.join(", ")}`
                : "No interfaces assigned"}
            </CardDescription>
            <CardAction>
              <Button
                size="sm"
                variant="outline"
                disabled={!editable}
                onClick={() => setAdding(zone.name)}
              >
                <PlusIcon data-icon="inline-start" />
                Allow
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="grid gap-4 sm:grid-cols-3">
            {(["service", "port", "source"] as const).map((kind) => {
              const values =
                kind === "service"
                  ? zone.services
                  : kind === "port"
                    ? zone.ports
                    : zone.sources
              return (
                <div key={kind} className="flex flex-col gap-2">
                  <span className="text-xs font-medium text-muted-foreground">
                    {kindLabels[kind]}
                  </span>
                  <div className="flex flex-wrap gap-1.5">
                    {values.length ? (
                      values.map((value) => (
                        <Badge
                          key={value}
                          variant="outline"
                          className="gap-1 pr-0.5 font-mono font-normal"
                        >
                          {value}
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            className="size-4 rounded-full"
                            disabled={!editable}
                            aria-label={`Remove ${value} from ${zone.name}`}
                            onClick={() =>
                              change.mutate({
                                action: `remove-${kind}`,
                                zone: zone.name,
                                [kind]: value,
                              })
                            }
                          >
                            <XIcon />
                          </Button>
                        </Badge>
                      ))
                    ) : (
                      <span className="text-sm text-muted-foreground">
                        None
                      </span>
                    )}
                  </div>
                </div>
              )
            })}
            {zone.rich.length > 0 && (
              <div className="flex flex-col gap-2 sm:col-span-3">
                <span className="text-xs font-medium text-muted-foreground">
                  Rich rules
                </span>
                {zone.rich.map((rule) => (
                  <code
                    key={rule}
                    className="rounded-md bg-muted px-2 py-1 text-xs break-all"
                  >
                    {rule}
                  </code>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      ))}
      {snapshot.backend === "UFW" && (
        <Card size="sm">
          <CardHeader>
            <CardTitle>Rules</CardTitle>
            <CardAction>
              <Button
                size="sm"
                variant="outline"
                disabled={!editable}
                onClick={() => setAdding("")}
              >
                <PlusIcon data-icon="inline-start" />
                Allow
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col gap-1">
            {snapshot.rules.length ? (
              snapshot.rules.map((rule) => (
                <code
                  key={rule}
                  className="rounded-md bg-muted px-2 py-1 text-xs"
                >
                  {rule.replace(/^UFW /, "")}
                </code>
              ))
            ) : (
              <span className="text-sm text-muted-foreground">No rules.</span>
            )}
          </CardContent>
        </Card>
      )}
      {adding !== undefined && (
        <AllowRule
          zone={adding}
          backend={snapshot.backend}
          change={change}
          onClose={() => setAdding(undefined)}
        />
      )}
    </div>
  )
}

function AllowRule({
  zone,
  backend,
  change,
  onClose,
}: {
  zone: string
  backend: string
  change: ReturnType<typeof useFirewallChange>
  onClose: () => void
}) {
  const [kind, setKind] = React.useState<RuleKind>(
    backend === "UFW" ? "port" : "service"
  )
  const [value, setValue] = React.useState("")
  const [protocol, setProtocol] = React.useState("tcp")
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form
          className="flex flex-col gap-5"
          onSubmit={(event) => {
            event.preventDefault()
            const payload =
              kind === "port" ? `${value.trim()}/${protocol}` : value.trim()
            change.mutate(
              {
                action: `add-${kind}`,
                zone: zone || undefined,
                [kind]: payload,
              },
              { onSuccess: onClose }
            )
          }}
        >
          <DialogHeader>
            <DialogTitle>Allow traffic{zone ? ` in ${zone}` : ""}</DialogTitle>
            <DialogDescription>
              The rule is saved and applied immediately.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <ToggleGroup
              value={[kind]}
              onValueChange={(next) => next[0] && setKind(next[0] as RuleKind)}
              variant="outline"
            >
              {backend !== "UFW" && (
                <ToggleGroupItem value="service">Service</ToggleGroupItem>
              )}
              <ToggleGroupItem value="port">Port</ToggleGroupItem>
              <ToggleGroupItem value="source">Source</ToggleGroupItem>
            </ToggleGroup>
            {kind === "port" ? (
              <Field orientation="horizontal">
                <Input
                  aria-label="Port"
                  required
                  inputMode="numeric"
                  pattern="[0-9]{1,5}"
                  placeholder="8080"
                  className="font-mono"
                  value={value}
                  onChange={(event) => setValue(event.target.value)}
                />
                <ToggleGroup
                  value={[protocol]}
                  onValueChange={(next) => next[0] && setProtocol(next[0])}
                  variant="outline"
                >
                  <ToggleGroupItem value="tcp">TCP</ToggleGroupItem>
                  <ToggleGroupItem value="udp">UDP</ToggleGroupItem>
                </ToggleGroup>
              </Field>
            ) : (
              <Field>
                <FieldLabel htmlFor="firewall-value">
                  {kind === "service" ? "Service name" : "Address or network"}
                </FieldLabel>
                <Input
                  id="firewall-value"
                  required
                  className="font-mono"
                  placeholder={kind === "service" ? "https" : "192.168.1.0/24"}
                  value={value}
                  onChange={(event) => setValue(event.target.value)}
                />
                {kind === "service" && (
                  <FieldDescription>
                    A firewalld service such as ssh, http, or cockpit.
                  </FieldDescription>
                )}
              </Field>
            )}
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={change.isPending}>
              {change.isPending && <Spinner data-icon="inline-start" />}
              Allow
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function FirewallGuard({
  state,
  onDone,
}: {
  state: FirewallState
  onDone: () => void
}) {
  const client = useQueryClient()
  const [now, setNow] = React.useState(Date.now())
  React.useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const deadline = state.rollbackDeadline
    ? Date.parse(state.rollbackDeadline)
    : undefined
  const remaining = deadline
    ? Math.max(0, Math.round((deadline - now) / 1000))
    : undefined
  const decide = useMutation({
    mutationFn: (action: "commit" | "rollback") =>
      api<FirewallState>("/firewall", {
        method: "POST",
        body: JSON.stringify({
          backend: backendOf(state.snapshot),
          action,
          checkpoint: state.checkpoint,
          rollbackToken: state.rollbackToken,
          confirmation: "CONFIRM FIREWALL CHANGE",
        } satisfies FirewallOperation),
      }),
    onSuccess: (_, action) => {
      void client.invalidateQueries({ queryKey: ["firewall"] })
      toast.add({
        title:
          action === "commit"
            ? "Firewall change kept"
            : "Firewall change reverted",
        type: "success",
      })
      onDone()
    },
    onError: (error) =>
      toast.add({
        title: "Could not finish the change",
        description: error.message,
        type: "error",
      }),
  })
  React.useEffect(() => {
    if (remaining !== 0) return
    void client.invalidateQueries({ queryKey: ["firewall"] })
    onDone()
  }, [remaining, onDone, client])
  return (
    <Alert className="border-warning/50">
      <ShieldAlertIcon />
      <AlertTitle>Keep this firewall change?</AlertTitle>
      <AlertDescription>
        {remaining !== undefined
          ? `It is reverted automatically in ${remaining}s, so you cannot lock yourself out.`
          : "It is reverted automatically unless you keep it."}
      </AlertDescription>
      <AlertAction className="flex gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={decide.isPending}
          onClick={() => decide.mutate("rollback")}
        >
          Revert
        </Button>
        <Button
          size="sm"
          disabled={decide.isPending}
          onClick={() => decide.mutate("commit")}
        >
          Keep
        </Button>
      </AlertAction>
    </Alert>
  )
}
