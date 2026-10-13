import { useMutation, useQueryClient } from "@tanstack/react-query"
import { ShieldAlertIcon } from "lucide-react"
import * as React from "react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  api,
  type InterfaceInfo,
  type NetworkOperation,
  type NetworkResponse,
  type NetworkState,
} from "@/lib/api"

function split(value: string) {
  return value
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

export function backendOf(
  snapshot?: NetworkResponse
): NetworkOperation["backend"] {
  const owner = snapshot?.ownership?.activeOwner
  return owner === "Netplan" ||
    owner === "systemd-networkd" ||
    owner === "ifupdown"
    ? owner
    : "NetworkManager"
}

// ConfigureInterface edits one interface's IPv4 addressing. The change is
// checkpointed: it rolls back on its own unless confirmed from the new state.
export function ConfigureInterface({
  snapshot,
  item,
  onClose,
  onPending,
}: {
  snapshot: NetworkResponse
  item: InterfaceInfo
  onClose: () => void
  onPending: (state: NetworkState) => void
}) {
  const client = useQueryClient()
  const ipv4 = item.addresses.filter(
    (address) => address.includes(".") && !address.startsWith("127.")
  )
  const [mode, setMode] = React.useState<"dhcp" | "static">("dhcp")
  const [addresses, setAddresses] = React.useState(ipv4.join(" "))
  const [gateway, setGateway] = React.useState(
    snapshot.routes?.find(
      (route) => route.destination === "default" && route.device === item.name
    )?.gateway ?? ""
  )
  const [dns, setDNS] = React.useState((snapshot.dns ?? []).join(" "))
  const apply = useMutation({
    mutationFn: async () => {
      const base = {
        backend: backendOf(snapshot),
        interface: item.name,
        connection: item.profile || undefined,
        expectedFingerprint: snapshot.fingerprint,
        confirmation: "CONFIRM NETWORK CHANGE",
      }
      const operation: NetworkOperation =
        mode === "dhcp"
          ? { ...base, action: "dhcp" }
          : {
              ...base,
              action: "static",
              addresses: split(addresses),
              address: split(addresses)[0],
              gateway: gateway || undefined,
              dns: split(dns),
            }
      return api<NetworkState>("/network", {
        method: "POST",
        body: JSON.stringify(operation),
      })
    },
    onSuccess: (state) => {
      void client.invalidateQueries({ queryKey: ["network"] })
      if (state.reconnectRequired) onPending(state)
      else toast.add({ title: `${item.name} updated`, type: "success" })
      onClose()
    },
  })
  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="w-full gap-0 sm:max-w-md">
        <SheetHeader className="border-b">
          <SheetTitle>Configure {item.name}</SheetTitle>
          <SheetDescription>
            {item.profile ? `Connection “${item.profile}”` : "IPv4 addressing"}
          </SheetDescription>
        </SheetHeader>
        <form
          id="network-configure"
          className="flex-1 overflow-auto p-4"
          onSubmit={(event) => {
            event.preventDefault()
            apply.mutate()
          }}
        >
          <FieldGroup>
            <Field>
              <FieldLabel>IPv4 addresses</FieldLabel>
              <ToggleGroup
                value={[mode]}
                onValueChange={(value) =>
                  value[0] && setMode(value[0] as typeof mode)
                }
                variant="outline"
              >
                <ToggleGroupItem value="dhcp">Automatic (DHCP)</ToggleGroupItem>
                <ToggleGroupItem value="static">Manual</ToggleGroupItem>
              </ToggleGroup>
            </Field>
            {mode === "static" && (
              <>
                <Field>
                  <FieldLabel htmlFor="net-addresses">Addresses</FieldLabel>
                  <Input
                    id="net-addresses"
                    required
                    className="font-mono"
                    placeholder="192.168.1.10/24"
                    value={addresses}
                    onChange={(event) => setAddresses(event.target.value)}
                  />
                  <FieldDescription>
                    One or more addresses with prefix length, separated by
                    spaces.
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="net-gateway">Gateway</FieldLabel>
                  <Input
                    id="net-gateway"
                    className="font-mono"
                    placeholder="192.168.1.1"
                    value={gateway}
                    onChange={(event) => setGateway(event.target.value)}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="net-dns">DNS servers</FieldLabel>
                  <Input
                    id="net-dns"
                    className="font-mono"
                    placeholder="1.1.1.1 9.9.9.9"
                    value={dns}
                    onChange={(event) => setDNS(event.target.value)}
                  />
                </Field>
              </>
            )}
            <Alert>
              <ShieldAlertIcon />
              <AlertTitle>Protected change</AlertTitle>
              <AlertDescription>
                If this change disconnects you, it is undone automatically after
                two minutes unless you confirm it.
              </AlertDescription>
            </Alert>
            {apply.isError && (
              <Alert variant="destructive">
                <AlertTitle>Nothing changed</AlertTitle>
                <AlertDescription>{apply.error.message}</AlertDescription>
              </Alert>
            )}
          </FieldGroup>
        </form>
        <SheetFooter className="flex-row justify-end border-t">
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            form="network-configure"
            disabled={apply.isPending}
          >
            {apply.isPending && <Spinner data-icon="inline-start" />}
            Apply
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

// PendingNetworkChange asks the operator to keep or revert a checkpointed
// change before its rollback deadline.
export function PendingNetworkChange({
  state,
  snapshot,
  onDone,
}: {
  state: NetworkState
  snapshot?: NetworkResponse
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
      api<NetworkState>("/network", {
        method: "POST",
        body: JSON.stringify({
          backend: backendOf(snapshot),
          action,
          checkpoint: state.checkpoint,
          reconnectToken: state.reconnectToken,
          confirmation: "CONFIRM NETWORK RECONNECT",
        } satisfies NetworkOperation),
      }),
    onSuccess: (_, action) => {
      void client.invalidateQueries({ queryKey: ["network"] })
      toast.add({
        title:
          action === "commit"
            ? "Network change kept"
            : "Network change reverted",
        type: "success",
      })
      onDone()
    },
  })
  React.useEffect(() => {
    if (remaining === 0) onDone()
  }, [remaining, onDone])
  return (
    <Alert className="border-warning/50">
      <ShieldAlertIcon />
      <AlertTitle>Keep this network change?</AlertTitle>
      <AlertDescription>
        {remaining !== undefined
          ? `It is reverted automatically in ${remaining}s.`
          : "It is reverted automatically soon."}{" "}
        {state.warning}
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
          Keep change
        </Button>
      </AlertAction>
    </Alert>
  )
}
