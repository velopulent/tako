import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  api,
  type KpatchOperation,
  type KpatchSettings,
  type KpatchStatus,
} from "@/lib/api"

export type KpatchResponse = {
  status: KpatchStatus
  settings: KpatchSettings
}

function stateLabel(settings: KpatchSettings) {
  if (settings.unavailable.length > 0) return "Not available"
  if (settings.missing.length > 0) return "Not installed"
  if (!settings.serviceEnabled) return "Disabled"
  return "Enabled"
}

export function KpatchSettingsCard({
  csrfToken,
  administrative,
}: {
  csrfToken: string
  administrative: boolean
}) {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = React.useState(false)
  const [apply, setApply] = React.useState(false)
  const [currentOnly, setCurrentOnly] = React.useState(false)

  const query = useQuery({
    queryKey: ["updates-kpatch"],
    queryFn: () => api<KpatchResponse>("/updates/kpatch"),
    staleTime: 5 * 60 * 1000,
  })

  const save = useMutation({
    mutationFn: (operation: KpatchOperation) =>
      api<KpatchSettings>("/updates/kpatch", {
        method: "PUT",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify(operation),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["updates-kpatch"] })
      setDialogOpen(false)
    },
  })

  // Kernel live patching is a RHEL-only feature; other distros never see it.
  if (!query.data?.settings?.supported) return null

  const settings = query.data.settings
  const status = query.data.status

  const openDialog = () => {
    setApply(settings.serviceEnabled)
    setCurrentOnly(!settings.auto && settings.serviceEnabled)
    setDialogOpen(true)
  }

  return (
    <div className="border-t pt-3">
      <div className="flex items-center justify-between">
        <div className="min-w-0">
          <p className="text-sm font-medium">Kernel live patching</p>
          <p className="truncate text-xs text-muted-foreground" id="kpatch-state">
            {stateLabel(settings)}
            {settings.kernel ? ` (${settings.kernel})` : ""}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={
            !administrative ||
            save.isPending ||
            settings.unavailable.length > 0 ||
            settings.missing.length > 0
          }
          onClick={openDialog}
        >
          {settings.serviceEnabled ? "Edit" : stateLabel(settings)}
        </Button>
      </div>

      {(status.loaded?.length ?? 0) > 0 ? (
        status.loaded.map((patch) => (
          <p key={patch} className="text-xs text-muted-foreground">
            Kernel live patch {patch} is active
          </p>
        ))
      ) : (
        (status.installed?.length ?? 0) > 0 &&
        status.installed.map((patch) => (
          <p key={patch} className="text-xs text-muted-foreground">
            Kernel live patch {patch} is installed
          </p>
        ))
      )}

      {settings.missing.length > 0 && (
        <p className="pt-2 text-xs text-muted-foreground">
          Install {settings.missing.join(" and ")} to configure kernel live patching.
        </p>
      )}
      {settings.unavailable.length > 0 && (
        <p className="pt-2 text-xs text-muted-foreground">
          Required packages are unavailable: {settings.unavailable.join(", ")}.{" "}
          <span className="sr-only">Not available</span>
        </p>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Kernel live patch settings</DialogTitle>
            <DialogDescription>
              Apply kernel security fixes without rebooting. Changes run through
              dnf kpatch and the kpatch service.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel>Live patching</FieldLabel>
              <ToggleGroup
                variant="outline"
                value={[apply ? "on" : "off"]}
                onValueChange={(values: string[]) =>
                  setApply((values[values.length - 1] ?? "off") === "on")
                }
              >
                <ToggleGroupItem value="off">Off</ToggleGroupItem>
                <ToggleGroupItem value="on">On</ToggleGroupItem>
              </ToggleGroup>
            </Field>
            {apply && (
              <Field>
                <FieldLabel>Apply patches for</FieldLabel>
                <ToggleGroup
                  variant="outline"
                  value={[currentOnly ? "current" : "future"]}
                  onValueChange={(values: string[]) =>
                    setCurrentOnly((values[values.length - 1] ?? "future") === "current")
                  }
                >
                  <ToggleGroupItem value="future">Current and future kernels</ToggleGroupItem>
                  <ToggleGroupItem value="current">
                    Current kernel only{settings.kernel ? ` (${settings.kernel})` : ""}
                  </ToggleGroupItem>
                </ToggleGroup>
              </Field>
            )}
            {!settings.patchInstalled && apply && currentOnly && !settings.patchUnavailable && (
              <Alert>
                <AlertTitle className="text-sm">
                  The patch package for this kernel will be installed.
                </AlertTitle>
                <AlertDescription className="text-xs">
                  {settings.patchName}
                </AlertDescription>
              </Alert>
            )}
            {apply && (
              <Alert>
                <AlertTitle className="text-sm">
                  This host will not need a reboot for kernel security fixes.
                </AlertTitle>
                <AlertDescription className="text-xs">
                  Reboot when convenient to move to kernels without live patches.
                </AlertDescription>
              </Alert>
            )}
          </FieldGroup>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)} disabled={save.isPending}>
              Cancel
            </Button>
            <Button onClick={() => save.mutate({ apply, currentOnly })} disabled={save.isPending || !administrative}>
              {save.isPending ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
          {save.error && (
            <p className="text-destructive text-xs">
              {save.error.message || "The configuration could not be applied."}
            </p>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
