import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CalendarClock } from "lucide-react"

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
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  api,
  type AutoUpdatesConfig,
  type AutoUpdatesOperation,
} from "@/lib/api"

const dayLabels: Record<AutoUpdatesConfig["day"], string> = {
  "": "Every day",
  mon: "Mondays",
  tue: "Tuesdays",
  wed: "Wednesdays",
  thu: "Thursdays",
  fri: "Fridays",
  sat: "Saturdays",
  sun: "Sundays",
}

type Mode = "off" | "security" | "all"

function describe(config: AutoUpdatesConfig) {
  if (!config.installed) return "Not set up"
  if (!config.enabled) return "Disabled"
  const when = `${dayLabels[config.day]?.toLowerCase() ?? "every day"} at ${config.time || "6:00"}`
  return config.type === "security"
    ? `Security updates will be applied ${when}`
    : `Updates will be applied ${when}`
}

export function AutoUpdatesCard({
  csrfToken,
  administrative,
  onConfigChanged,
}: {
  csrfToken: string
  administrative: boolean
  onConfigChanged?: (enabled: boolean) => void
}) {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = React.useState(false)
  const [mode, setMode] = React.useState<Mode>("off")
  const [day, setDay] = React.useState<AutoUpdatesConfig["day"]>("")
  const [time, setTime] = React.useState("06:00")

  const query = useQuery({
    queryKey: ["updates-automatic"],
    queryFn: () => api<AutoUpdatesConfig>("/updates/automatic"),
  })

  const save = useMutation({
    mutationFn: (operation: AutoUpdatesOperation) =>
      api<AutoUpdatesConfig>("/updates/automatic", {
        method: "PUT",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify(operation),
      }),
    onSuccess: (config) => {
      queryClient.setQueryData(["updates-automatic"], config)
      onConfigChanged?.(config.enabled)
      setDialogOpen(false)
    },
  })

  if (query.isPending) return null
  // No provider for this distro family yet (apt/zypper/alpm adapters land
  // later): keep the card honest instead of pretending to configure.
  if (!query.data?.available) {
    return (
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm font-medium">Automatic updates</p>
          <p className="text-xs text-muted-foreground">Not available</p>
        </div>
      </div>
    )
  }

  const config = query.data

  const openDialog = () => {
    setMode(config.enabled ? config.type : "off")
    setDay(config.day)
    setTime(config.time ? config.time.padStart(5, "0") : "06:00")
    setDialogOpen(true)
  }

  const submit = () => {
    const operation: AutoUpdatesOperation =
      mode === "off"
        ? { enabled: false }
        : { enabled: true, type: mode, day, time }
    save.mutate(operation)
  }

  return (
    <>
      <div className="flex items-center justify-between">
        <div className="min-w-0">
          <p className="flex items-center gap-1.5 text-sm font-medium">
            <CalendarClock className="size-4 text-muted-foreground" />
            Automatic updates
          </p>
          <p className="truncate text-xs text-muted-foreground">
            {describe(config)}
          </p>
        </div>
        {!administrative || save.isPending ? (
          <Button variant="outline" size="sm" disabled={!administrative} onClick={openDialog}>
            {config.installed ? "Edit" : "Enable"}
          </Button>
        ) : (
          <Button variant="outline" size="sm" onClick={openDialog}>
            {config.installed ? "Edit" : "Enable"}
          </Button>
        )}
      </div>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Automatic updates</DialogTitle>
            <DialogDescription>
              Applies through the system package manager ({config.provider}).
              Changes take effect on the host immediately.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel>Update policy</FieldLabel>
              <ToggleGroup
                variant="outline"
                value={[mode]}
                onValueChange={(values: string[]) =>
                  setMode((values[values.length - 1] as Mode) ?? "off")
                }
              >
                <ToggleGroupItem value="off">No updates</ToggleGroupItem>
                <ToggleGroupItem value="security">
                  Security only
                </ToggleGroupItem>
                <ToggleGroupItem value="all">All updates</ToggleGroupItem>
              </ToggleGroup>
            </Field>
            {mode !== "off" && (
              <>
                <Field>
                  <FieldLabel htmlFor="auto-update-day">When</FieldLabel>
                  <div className="flex gap-2">
                    <Select
                      value={day === "" ? "everyday" : day}
                      onValueChange={(value: string | null) => {
                        if (value === null) return
                        setDay(value === "everyday" ? "" : (value as AutoUpdatesConfig["day"]))
                      }}
                    >
                      <SelectTrigger id="auto-update-day" className="w-40">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="everyday">Every day</SelectItem>
                        {Object.entries(dayLabels)
                          .filter(([value]) => value !== "")
                          .map(([value, label]) => (
                            <SelectItem key={value} value={value}>
                              {label}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                    <Input
                      id="auto-update-time"
                      type="time"
                      value={time}
                      onChange={(event) => setTime(event.target.value)}
                      aria-label="Update time of day"
                    />
                  </div>
                </Field>
                <Alert>
                  <AlertTitle className="text-sm">
                    This host will reboot after updates are installed.
                  </AlertTitle>
                  <AlertDescription className="text-xs">
                    The package manager is configured to reboot when required.
                  </AlertDescription>
                </Alert>
              </>
            )}
          </FieldGroup>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDialogOpen(false)}
              disabled={save.isPending}
            >
              Cancel
            </Button>
            <Button onClick={submit} disabled={save.isPending || !administrative}>
              {save.isPending ? "Saving…" : "Save changes"}
            </Button>
          </DialogFooter>
          {save.error && (
            <p className="text-destructive text-xs">
              {save.error.message || "The configuration could not be applied."}
            </p>
          )}
          {!administrative && (
            <p className="text-xs text-muted-foreground">
              Gain Administrative access to change automatic updates.
            </p>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}
