import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import type { NetworkResponse } from "@/lib/api"

function isIPv4Family(family: string) {
  const normalized = family.toLowerCase()
  return (
    normalized === "inet" ||
    normalized === "ipv4" ||
    normalized === "v4" ||
    normalized.endsWith("4")
  )
}

function isIPv6Family(family: string) {
  const normalized = family.toLowerCase()
  return (
    normalized === "inet6" ||
    normalized === "ipv6" ||
    normalized === "v6" ||
    normalized.endsWith("6")
  )
}

export function NetworkSummary({ snapshot }: { snapshot?: NetworkResponse }) {
  const interfaces = snapshot?.items ?? []
  const addresses = snapshot?.addresses ?? []
  const routes = snapshot?.routes ?? []
  const dns = snapshot?.dns ?? []
  const up = interfaces.filter((item) => item.up).length
  const ipv4 = addresses.filter((item) => isIPv4Family(item.family)).length
  const ipv6 = addresses.filter((item) => isIPv6Family(item.family)).length
  const owner = snapshot?.ownership?.activeOwner || "Unknown"
  const detected =
    snapshot?.ownership?.detected?.join(", ") || "No ownership metadata"

  return (
    <section
      aria-label="Network status summary"
      className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4"
    >
      <Card>
        <CardHeader className="gap-1">
          <CardTitle className="text-sm">Network owner</CardTitle>
          <p className="text-2xl font-semibold tracking-tight">{owner}</p>
          <CardDescription>Detected: {detected}</CardDescription>
        </CardHeader>
      </Card>
      <Card>
        <CardHeader className="gap-1">
          <CardTitle className="text-sm">Interfaces</CardTitle>
          <p className="text-2xl font-semibold tracking-tight">
            {up}/{interfaces.length} up
          </p>
          <CardDescription>
            {interfaces.length} device(s) reported
          </CardDescription>
        </CardHeader>
      </Card>
      <Card>
        <CardHeader className="gap-1">
          <CardTitle className="text-sm">IP addresses</CardTitle>
          <p className="text-2xl font-semibold tracking-tight">
            {addresses.length}
          </p>
          <CardDescription>
            {ipv4} IPv4 · {ipv6} IPv6
          </CardDescription>
        </CardHeader>
      </Card>
      <Card>
        <CardHeader className="gap-1">
          <CardTitle className="text-sm">Routing and DNS</CardTitle>
          <p className="text-2xl font-semibold tracking-tight">
            {routes.length} routes
          </p>
          <CardDescription>{dns.length} DNS server(s)</CardDescription>
        </CardHeader>
      </Card>
      {snapshot?.ownership?.conflicted && (
        <Alert variant="destructive" className="sm:col-span-2 xl:col-span-4">
          <AlertTitle>Ownership conflict</AlertTitle>
          <AlertDescription>
            {snapshot.ownership.reason ||
              "Multiple network managers reported ownership of this host."}
          </AlertDescription>
        </Alert>
      )}
    </section>
  )
}

export function NetworkDetails({ snapshot }: { snapshot: NetworkResponse }) {
  const addresses = snapshot.addresses ?? []
  const routes = snapshot.routes ?? []
  const dns = snapshot.dns ?? []

  return (
    <section aria-label="Network details" className="grid gap-4 xl:grid-cols-3">
      <Card>
        <CardHeader>
          <CardTitle>IP addresses</CardTitle>
          <CardDescription>
            Global and interface addresses by family and scope.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {addresses.length > 0 ? (
            <Table containerClassName="max-h-64">
              <TableHeader>
                <TableRow>
                  <TableHead>Interface</TableHead>
                  <TableHead>Address</TableHead>
                  <TableHead>Family</TableHead>
                  <TableHead>Scope</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {addresses.map((item) => (
                  <TableRow key={[item.interface, item.address].join(":")}>
                    <TableCell>{item.interface}</TableCell>
                    <TableCell className="font-mono">{item.address}</TableCell>
                    <TableCell>{item.family}</TableCell>
                    <TableCell>{item.scope || "—"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-sm text-muted-foreground">
              No addresses reported.
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Routes</CardTitle>
          <CardDescription>
            Kernel routes, gateways, devices, and metrics.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {routes.length > 0 ? (
            <Table containerClassName="max-h-64">
              <TableHeader>
                <TableRow>
                  <TableHead>Destination</TableHead>
                  <TableHead>Gateway</TableHead>
                  <TableHead>Device</TableHead>
                  <TableHead>Metric</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {routes.map((item, index) => (
                  <TableRow
                    key={[
                      item.destination,
                      item.gateway,
                      item.device,
                      index,
                    ].join(":")}
                  >
                    <TableCell className="font-mono">
                      {item.destination}
                    </TableCell>
                    <TableCell className="font-mono">
                      {item.gateway || "—"}
                    </TableCell>
                    <TableCell>{item.device || "—"}</TableCell>
                    <TableCell>{item.metric ?? "—"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-sm text-muted-foreground">No routes reported.</p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>DNS servers</CardTitle>
          <CardDescription>
            Resolver addresses reported by the host.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {dns.length > 0 ? (
            <ul className="flex flex-wrap gap-2">
              {dns.map((server, index) => (
                <li key={[server, index].join(":")}>
                  <Badge variant="outline" className="font-mono">
                    {server}
                  </Badge>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">
              No DNS servers reported.
            </p>
          )}
        </CardContent>
      </Card>
    </section>
  )
}
