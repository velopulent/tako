import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import type { HostInfo } from "@/lib/api"

const bytes = (value = 0) => {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  let amount = value
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index += 1
  }
  return `${amount.toFixed(index ? 1 : 0)} ${units[index]}`
}

export function HostInventory({ host }: { host: HostInfo }) {
  return (
    <section
      aria-label="Host inventory"
      className="grid gap-4 @4xl/main:grid-cols-3"
    >
      <Card>
        <CardHeader>
          <CardTitle>Hardware</CardTitle>
          <CardDescription>Detected CPU and memory capacity.</CardDescription>
        </CardHeader>
        <CardContent>
          {host.hardware.available ? (
            <dl className="grid gap-3 text-sm sm:grid-cols-2 @4xl/main:grid-cols-1">
              <div>
                <dt className="text-muted-foreground">Processor</dt>
                <dd className="font-medium">
                  {host.hardware.cpuModel || "Unknown"}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">CPU cores</dt>
                <dd className="font-medium">{host.hardware.cpuCores ?? "—"}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Memory</dt>
                <dd className="font-medium">
                  {bytes(host.hardware.memoryTotal)}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Available memory</dt>
                <dd className="font-medium">
                  {bytes(host.hardware.memoryAvailable)}
                </dd>
              </div>
            </dl>
          ) : (
            <p className="text-sm text-muted-foreground">
              {host.hardware.reason || "Hardware details are unavailable."}
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Boot and shutdown</CardTitle>
          <CardDescription>
            Current boot and previous shutdown evidence.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <div>
            <p className="text-muted-foreground">Boot ID</p>
            <p className="truncate font-mono text-xs" title={host.bootId}>
              {host.bootId || "Unavailable"}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-muted-foreground">Previous shutdown</span>
            <Badge
              variant={
                !host.shutdown.available
                  ? "outline"
                  : host.shutdown.clean
                    ? "secondary"
                    : "destructive"
              }
            >
              {!host.shutdown.available
                ? "Unknown"
                : host.shutdown.clean
                  ? "Clean"
                  : "Unclean"}
            </Badge>
          </div>
          {!host.shutdown.available && host.shutdown.reason && (
            <p className="text-xs text-muted-foreground">
              {host.shutdown.reason}
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Restart status</CardTitle>
          <CardDescription>
            Whether the host needs a reboot to finish updates.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {host.restart.available ? (
            <div className="space-y-3 text-sm">
              <Badge
                variant={host.restart.required ? "destructive" : "secondary"}
              >
                {host.restart.required
                  ? "Restart required"
                  : "No restart required"}
              </Badge>
              <p className="text-muted-foreground">
                {host.restart.reason ||
                  `Reported by ${host.restart.source || "the operating system"}.`}
              </p>
            </div>
          ) : (
            <Alert>
              <AlertTitle>Unknown</AlertTitle>
              <AlertDescription>
                {host.restart.reason ||
                  "No restart-status provider is available."}
              </AlertDescription>
            </Alert>
          )}
        </CardContent>
      </Card>
    </section>
  )
}
