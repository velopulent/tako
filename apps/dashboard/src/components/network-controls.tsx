import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import * as React from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
  type NetworkOperation,
  type NetworkResponse,
  type NetworkState,
  type SessionResponse,
} from "@/lib/api"

type NetworkMutation = Exclude<
  NetworkOperation["action"],
  "preview" | "checkpoint" | "commit" | "rollback"
>

function backendValue(response?: NetworkResponse): NetworkOperation["backend"] {
  const owner = response?.ownership?.activeOwner
  if (
    owner === "NetworkManager" ||
    owner === "Netplan" ||
    owner === "systemd-networkd" ||
    owner === "ifupdown"
  ) {
    return owner
  }
  return "NetworkManager"
}

export function NetworkControls() {
  const queryClient = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const network = useQuery({
    queryKey: ["network"],
    queryFn: () => api<NetworkResponse>("/network"),
  })
  const [action, setAction] = React.useState<NetworkMutation>("dhcp")
  const [iface, setIface] = React.useState("")
  const [connection, setConnection] = React.useState("")
  const [addresses, setAddresses] = React.useState("")
  const [gateway, setGateway] = React.useState("")
  const [dns, setDNS] = React.useState("")
  const [route, setRoute] = React.useState("")
  const [metric, setMetric] = React.useState("")
  const [confirmation, setConfirmation] = React.useState("")
  const [pending, setPending] = React.useState<NetworkState>()
  const isRoute = action === "route-add" || action === "route-remove"
  const operation = React.useCallback(
    (requestedAction: NetworkOperation["action"]): NetworkOperation => {
      const checkpointAction =
        requestedAction === "commit" || requestedAction === "rollback"
      if (checkpointAction) {
        return {
          backend: backendValue(network.data),
          action: requestedAction,
          confirmation: confirmation || undefined,
          reconnectToken: pending?.reconnectToken,
          checkpoint: pending?.checkpoint,
        }
      }
      return {
        backend: backendValue(network.data),
        action: requestedAction,
        interface: iface || undefined,
        connection: connection || undefined,
        addresses: addresses
          .split(/[\s,]+/)
          .map((value) => value.trim())
          .filter(Boolean),
        address: addresses.split(/[\s,]+/).filter(Boolean)[0],
        gateway: gateway || undefined,
        dns: dns
          .split(/[\s,]+/)
          .map((value) => value.trim())
          .filter(Boolean),
        route: isRoute ? route || undefined : undefined,
        metric: metric ? Number(metric) : undefined,
        expectedFingerprint: network.data?.fingerprint,
        confirmation: confirmation || undefined,
      }
    },
    [
      addresses,
      confirmation,
      connection,
      dns,
      gateway,
      iface,
      isRoute,
      metric,
      network.data,
      pending,
      route,
    ]
  )
  const preview = useMutation({
    mutationFn: () =>
      api<NetworkState>("/network/preview", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation(action)),
      }),
  })
  const apply = useMutation({
    mutationFn: (value: NetworkOperation) =>
      api<NetworkState>("/network", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(value),
      }),
    onSuccess: (state) => {
      setPending(state.reconnectRequired ? state : undefined)
      void queryClient.invalidateQueries({ queryKey: ["network"] })
    },
  })
  const needsAddress = action === "static"
  const needsDNS = action === "dns"
  const needsRoute = isRoute
  const invalidInput =
    !iface ||
    (needsAddress && !addresses) ||
    (needsDNS && !dns) ||
    (needsRoute && !route) ||
    !confirmation
  const blocked =
    invalidInput || !network.data || preview.isPending || apply.isPending

  return (
    <Card>
      <CardHeader>
        <CardTitle>Network configuration</CardTitle>
        <CardDescription>
          The detected owner is selected automatically. Every mutation is
          checkpointed; verify a fresh connection before commit.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {network.data?.ownership?.conflicted && (
          <Alert variant="destructive">
            <AlertTitle>Ownership conflict</AlertTitle>
            <AlertDescription>{network.data.ownership.reason}</AlertDescription>
          </Alert>
        )}
        <FieldGroup className="grid gap-4 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="network-backend">Detected backend</FieldLabel>
            <Input
              id="network-backend"
              value={backendValue(network.data)}
              readOnly
            />
            <FieldDescription>
              Detected:{" "}
              {network.data?.ownership?.detected?.join(", ") || "kernel-only"}
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="network-action">Action</FieldLabel>
            <select
              id="network-action"
              className="h-9 rounded-md border bg-background px-3 text-sm"
              value={action}
              onChange={(event) => {
                setAction(event.target.value as NetworkMutation)
                setConfirmation("")
              }}
            >
              <option value="dhcp">DHCP</option>
              <option value="static">Static addresses</option>
              <option value="dns">DNS</option>
              <option value="route-add">Add route</option>
              <option value="route-remove">Remove route</option>
            </select>
          </Field>
          <Field>
            <FieldLabel htmlFor="network-interface">Interface</FieldLabel>
            <Input
              id="network-interface"
              value={iface}
              onChange={(event) => setIface(event.target.value)}
              placeholder="eno1"
            />
            <FieldDescription>Use an interface listed above.</FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="network-connection">
              Connection profile
            </FieldLabel>
            <Input
              id="network-connection"
              value={connection}
              onChange={(event) => setConnection(event.target.value)}
              placeholder="Wired connection 1"
            />
            <FieldDescription>
              Optional; NetworkManager uses the interface when blank.
            </FieldDescription>
          </Field>
          {needsAddress && (
            <Field className="md:col-span-2">
              <FieldLabel htmlFor="network-addresses">Addresses</FieldLabel>
              <Input
                id="network-addresses"
                value={addresses}
                onChange={(event) => setAddresses(event.target.value)}
                placeholder="192.0.2.10/24 2001:db8::10/64"
              />
            </Field>
          )}
          {(needsAddress || needsRoute) && (
            <Field>
              <FieldLabel htmlFor="network-gateway">Gateway</FieldLabel>
              <Input
                id="network-gateway"
                value={gateway}
                onChange={(event) => setGateway(event.target.value)}
                placeholder="192.0.2.1"
              />
            </Field>
          )}
          {needsDNS && (
            <Field>
              <FieldLabel htmlFor="network-dns">DNS servers</FieldLabel>
              <Input
                id="network-dns"
                value={dns}
                onChange={(event) => setDNS(event.target.value)}
                placeholder="1.1.1.1 9.9.9.9"
              />
            </Field>
          )}
          {needsRoute && (
            <>
              <Field>
                <FieldLabel htmlFor="network-route">Route</FieldLabel>
                <Input
                  id="network-route"
                  value={route}
                  onChange={(event) => setRoute(event.target.value)}
                  placeholder="default or 192.0.2.0/24"
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="network-metric">Metric</FieldLabel>
                <Input
                  id="network-metric"
                  type="number"
                  min={0}
                  max={65535}
                  value={metric}
                  onChange={(event) => setMetric(event.target.value)}
                  placeholder="100"
                />
              </Field>
            </>
          )}
          <Field className="md:col-span-2">
            <FieldLabel htmlFor="network-confirmation">Confirmation</FieldLabel>
            <Input
              id="network-confirmation"
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              placeholder="CONFIRM NETWORK CHANGE"
            />
            <FieldDescription>
              Reconnect confirmation is required before commit or rollback.
            </FieldDescription>
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
            Apply {action.replaceAll("-", " ")}
          </Button>
        </div>
        {pending?.reconnectRequired && (
          <Alert>
            <AlertTitle>Reconnect checkpoint active</AlertTitle>
            <AlertDescription className="space-y-2">
              <span className="block">{pending.warning}</span>
              <span className="block font-mono text-xs">
                Deadline:{" "}
                {pending.rollbackDeadline
                  ? new Date(pending.rollbackDeadline).toLocaleString()
                  : "pending"}
              </span>
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  onClick={() => apply.mutate(operation("commit"))}
                  disabled={apply.isPending}
                >
                  Commit after reconnect
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
        {preview.data && (
          <Alert>
            <AlertTitle>Preview ready</AlertTitle>
            <AlertDescription>
              {preview.data.warning ??
                "Review the owner and checkpoint before applying."}
            </AlertDescription>
          </Alert>
        )}
        {(preview.isError || apply.isError) && (
          <Alert variant="destructive">
            <AlertTitle>Network change unavailable</AlertTitle>
            <AlertDescription>
              Ownership, authority, or the fingerprint may have changed. Refresh
              before retrying.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}
