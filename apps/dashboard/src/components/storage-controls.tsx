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
  type SessionResponse,
  type StorageInventoryResponse,
  type StorageOperation,
  type StorageState,
} from "@/lib/api"

type StorageAction = Exclude<StorageOperation["action"], "preview">

export function StorageControls({
  snapshot,
}: {
  snapshot: StorageInventoryResponse
}) {
  const client = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const partitions = snapshot.devices.flatMap((device) =>
    device.partitions.map((partition) => ({
      ...partition,
      device: device.path,
      label: `${partition.path}${partition.label ? ` · ${partition.label}` : ""}`,
    }))
  )
  const [device, setDevice] = React.useState("")
  const [action, setAction] = React.useState<StorageAction>("mount")
  const [target, setTarget] = React.useState("")
  const [filesystem, setFilesystem] = React.useState("")
  const [confirmation, setConfirmation] = React.useState("")
  const selected = partitions.find((partition) => partition.path === device)

  React.useEffect(() => {
    if (!device && partitions[0]) {
      setDevice(partitions[0].path)
    }
  }, [device, partitions])

  React.useEffect(() => {
    if (!selected) return
    if (!filesystem) setFilesystem(selected.filesystem ?? "")
    if (!target && selected.mountPoints[0])
      setTarget(selected.mountPoints[0].target)
  }, [filesystem, selected, target])

  const operation = React.useCallback(
    (requestedAction: StorageOperation["action"]): StorageOperation => ({
      action: requestedAction,
      device,
      target: action === "mount" ? undefined : target || undefined,
      filesystem: filesystem || undefined,
      expectedFingerprint: snapshot.fingerprint,
      confirmation: confirmation || undefined,
    }),
    [action, confirmation, device, filesystem, snapshot.fingerprint, target]
  )
  const preview = useMutation({
    mutationFn: () =>
      api<StorageState>("/storage/preview", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation(action)),
      }),
  })
  const apply = useMutation({
    mutationFn: () =>
      api<StorageState>("/storage", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation(action)),
      }),
    onSuccess: () => {
      preview.reset()
      void client.invalidateQueries({ queryKey: ["storage"] })
    },
  })
  const persistent = action.startsWith("persistent-")
  const needsTarget =
    action === "persistent-mount" || action === "persistent-unmount"
  const needsFilesystem = action === "persistent-mount"
  const blocked =
    snapshot.readOnly ||
    !device ||
    (needsTarget && !target) ||
    (needsFilesystem && !filesystem) ||
    !confirmation ||
    preview.isPending ||
    apply.isPending

  return (
    <Card>
      <CardHeader>
        <CardTitle>Mount controls</CardTitle>
        <CardDescription>
          UDisks2 performs the live mount change. Persistent edits touch only
          Tako-owned /etc/fstab entries and are fingerprint checked.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {snapshot.readOnly && (
          <Alert>
            <AlertTitle>Read-only inventory</AlertTitle>
            <AlertDescription>
              {snapshot.reason ?? "Storage mutations are unavailable."}
            </AlertDescription>
          </Alert>
        )}
        {partitions.length === 0 && (
          <Alert>
            <AlertTitle>No mountable partitions reported</AlertTitle>
            <AlertDescription>
              Refresh after attaching a filesystem-backed block device.
            </AlertDescription>
          </Alert>
        )}
        <FieldGroup className="grid gap-4 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="storage-device">Partition</FieldLabel>
            <select
              id="storage-device"
              className="h-9 rounded-md border bg-background px-3 text-sm"
              value={device}
              onChange={(event) => {
                setDevice(event.target.value)
                const next = partitions.find(
                  (item) => item.path === event.target.value
                )
                setFilesystem(next?.filesystem ?? "")
                setTarget(next?.mountPoints[0]?.target ?? "")
              }}
            >
              <option value="">Select a partition</option>
              {partitions.map((partition) => (
                <option key={partition.path} value={partition.path}>
                  {partition.label}
                </option>
              ))}
            </select>
          </Field>
          <Field>
            <FieldLabel htmlFor="storage-action">Action</FieldLabel>
            <select
              id="storage-action"
              className="h-9 rounded-md border bg-background px-3 text-sm"
              value={action}
              onChange={(event) => {
                const next = event.target.value as StorageAction
                setAction(next)
                setConfirmation("")
              }}
            >
              <option value="mount">Mount</option>
              <option value="unmount">Unmount</option>
              <option value="persistent-mount">Persistent mount</option>
              <option value="persistent-unmount">Persistent unmount</option>
            </select>
            <FieldDescription>
              {persistent
                ? "Requires a persistent-mount confirmation phrase."
                : "Protected system targets are rejected."}
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="storage-target">Mount target</FieldLabel>
            <Input
              id="storage-target"
              value={target}
              onChange={(event) => setTarget(event.target.value)}
              placeholder="/mnt/data"
              disabled={action === "mount"}
            />
            {action === "mount" && (
              <FieldDescription>
                UDisks2 chooses the live mount location. Use persistent mount
                for an exact target.
              </FieldDescription>
            )}
          </Field>
          <Field>
            <FieldLabel htmlFor="storage-filesystem">Filesystem</FieldLabel>
            <Input
              id="storage-filesystem"
              value={filesystem}
              onChange={(event) => setFilesystem(event.target.value)}
              placeholder="ext4"
              disabled={!needsFilesystem}
            />
          </Field>
          <Field className="md:col-span-2">
            <FieldLabel htmlFor="storage-confirmation">Confirmation</FieldLabel>
            <Input
              id="storage-confirmation"
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              placeholder={
                persistent
                  ? "CONFIRM PERSISTENT MOUNT"
                  : "CONFIRM STORAGE CHANGE"
              }
            />
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
          <Button disabled={blocked} onClick={() => apply.mutate()}>
            Apply {action.replaceAll("-", " ")}
          </Button>
        </div>
        {selected && (
          <div className="flex flex-wrap gap-2 text-sm text-muted-foreground">
            <Badge variant="outline">
              {selected.filesystem || "unknown fs"}
            </Badge>
            <span>
              {selected.mountPoints.length
                ? selected.mountPoints.map((mount) => mount.target).join(", ")
                : "not mounted"}
            </span>
          </div>
        )}
        {preview.data && (
          <Alert>
            <AlertTitle>Preview ready</AlertTitle>
            <AlertDescription>
              {preview.data.warning ?? "Re-read the inventory before applying."}
            </AlertDescription>
          </Alert>
        )}
        {(preview.isError || apply.isError) && (
          <Alert variant="destructive">
            <AlertTitle>Storage change unavailable</AlertTitle>
            <AlertDescription>
              Refresh inventory and verify authority, fingerprint, target, and
              confirmation before retrying.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}
