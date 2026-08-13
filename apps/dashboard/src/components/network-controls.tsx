import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import {
  api,
  type NetworkOperation,
  type NetworkResponse,
  type NetworkState,
  type SessionResponse,
} from "@/lib/api"
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
  const [iface, setIface] = React.useState("")
  const [address, setAddress] = React.useState("")
  const [gateway, setGateway] = React.useState("")
  const [confirmation, setConfirmation] = React.useState("")
  const operation = (action: NetworkOperation["action"]): NetworkOperation => ({
    backend: "NetworkManager",
    action,
    interface: iface,
    address: action === "static" ? address : undefined,
    gateway: action === "static" ? gateway : undefined,
    expectedFingerprint: network.data?.fingerprint,
    confirmation,
  })
  const preview = useMutation({
    mutationFn: (value: NetworkOperation) =>
      api<NetworkState>("/network/preview", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(value),
      }),
  })
  const apply = useMutation({
    mutationFn: (value: NetworkOperation) =>
      api<NetworkState>("/network", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(value),
      }),
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: ["network"] }),
  })
  const owner = network.data?.ownership
  return (
    <Card>
      <CardHeader>
        <CardTitle>Network configuration</CardTitle>
        <CardDescription>
          Changes are restricted to an active NetworkManager owner and require a
          fresh fingerprint.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {owner?.conflicted && (
          <Alert variant="destructive">
            <AlertTitle>Ownership conflict</AlertTitle>
            <AlertDescription>{owner.reason}</AlertDescription>
          </Alert>
        )}
        <FieldGroup className="grid gap-4 md:grid-cols-2">
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
            <FieldLabel htmlFor="network-address">Static address</FieldLabel>
            <Input
              id="network-address"
              value={address}
              onChange={(event) => setAddress(event.target.value)}
              placeholder="192.0.2.10/24"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="network-gateway">Gateway</FieldLabel>
            <Input
              id="network-gateway"
              value={gateway}
              onChange={(event) => setGateway(event.target.value)}
              placeholder="192.0.2.1"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="network-confirmation">Confirmation</FieldLabel>
            <Input
              id="network-confirmation"
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              placeholder="CONFIRM NETWORK CHANGE"
            />
            <FieldDescription>Required for any mutation.</FieldDescription>
          </Field>
        </FieldGroup>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={!iface || preview.isPending}
            onClick={() => preview.mutate(operation("dhcp"))}
          >
            Preview DHCP
          </Button>
          <Button
            disabled={!iface || !address || apply.isPending}
            onClick={() => apply.mutate(operation("static"))}
          >
            Apply static address
          </Button>
        </div>
        {preview.data && (
          <Alert>
            <AlertTitle>Preview ready</AlertTitle>
            <AlertDescription>
              {preview.data.warning ??
                "Review ownership and reconnect before applying."}
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
