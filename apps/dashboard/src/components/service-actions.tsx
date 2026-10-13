import { useMutation, useQueryClient } from "@tanstack/react-query"
import {
  ChevronDownIcon,
  PlayIcon,
  RotateCwIcon,
  SquareIcon,
} from "lucide-react"
import * as React from "react"

import { useAdminAccess } from "@/components/admin-access"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { ButtonGroup } from "@/components/ui/button-group"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { api, type ServiceDetail, type ServiceImpact } from "@/lib/api"
import type { ServiceActionName } from "@/lib/service-actions"
import { useSession } from "@/lib/session"

const disruptive = new Set<ServiceActionName>([
  "stop",
  "restart",
  "disable",
  "mask",
])

const labels: Record<ServiceActionName, string> = {
  start: "Start",
  stop: "Stop",
  restart: "Restart",
  reload: "Reload",
  enable: "Start at boot",
  disable: "Don't start at boot",
  mask: "Mask",
  unmask: "Unmask",
}

const past: Record<ServiceActionName, string> = {
  start: "started",
  stop: "stopped",
  restart: "restarted",
  reload: "reloaded",
  enable: "enabled",
  disable: "disabled",
  mask: "masked",
  unmask: "unmasked",
}

// ServiceActions shows the controls that make sense for the unit's state.
// Disruptive actions open a confirmation with the dependency impact.
export function ServiceActions({ unit }: { unit: ServiceDetail }) {
  const client = useQueryClient()
  const session = useSession()
  const { administrative } = useAdminAccess()
  const [confirming, setConfirming] = React.useState<ServiceActionName | null>(
    null
  )
  const path = `/services/${unit.scope}/${encodeURIComponent(unit.name)}`
  const allowed = unit.scope === "user" ? Boolean(session) : administrative

  const action = useMutation({
    mutationFn: (name: ServiceActionName) =>
      api<ServiceDetail>(`${path}/actions`, {
        method: "POST",
        body: JSON.stringify({ action: name }),
      }),
    onSuccess: (value, name) => {
      client.setQueryData(["service", unit.scope, unit.name], value)
      void client.invalidateQueries({ queryKey: ["services"] })
      setConfirming(null)
      toast.add({ title: `${unit.name} ${past[name]}`, type: "success" })
    },
    onError: (error, name) => {
      setConfirming(null)
      toast.add({
        title: `Could not ${labels[name].toLowerCase()} ${unit.name}`,
        description: error.message,
        type: "error",
      })
    },
  })

  const run = (name: ServiceActionName) =>
    disruptive.has(name) ? setConfirming(name) : action.mutate(name)
  const active =
    unit.activeState === "active" ||
    unit.activeState === "activating" ||
    unit.activeState === "reloading"
  const masked = unit.fileState === "masked"
  const enabled = unit.fileState === "enabled"
  const startable = unit.fileState !== "static" || unit.type === "service"
  const busy = action.isPending

  return (
    <>
      <ButtonGroup>
        {active ? (
          <Button
            size="sm"
            variant="outline"
            disabled={!allowed || busy}
            onClick={() => run("stop")}
          >
            {busy && action.variables === "stop" ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <SquareIcon data-icon="inline-start" />
            )}
            Stop
          </Button>
        ) : (
          <Button
            size="sm"
            variant="outline"
            disabled={!allowed || busy || masked || !startable}
            onClick={() => run("start")}
          >
            {busy && action.variables === "start" ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <PlayIcon data-icon="inline-start" />
            )}
            Start
          </Button>
        )}
        <Button
          size="sm"
          variant="outline"
          disabled={!allowed || busy || masked}
          onClick={() => run("restart")}
        >
          {busy && action.variables === "restart" ? (
            <Spinner data-icon="inline-start" />
          ) : (
            <RotateCwIcon data-icon="inline-start" />
          )}
          Restart
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                size="sm"
                variant="outline"
                aria-label="More actions"
                disabled={!allowed || busy}
              />
            }
          >
            <ChevronDownIcon />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-52">
            <DropdownMenuGroup>
              <DropdownMenuItem
                disabled={!active}
                onClick={() => run("reload")}
              >
                Reload configuration
              </DropdownMenuItem>
              {enabled ? (
                <DropdownMenuItem onClick={() => run("disable")}>
                  Don't start at boot
                </DropdownMenuItem>
              ) : (
                <DropdownMenuItem
                  disabled={masked || unit.fileState === "static"}
                  onClick={() => run("enable")}
                >
                  Start at boot
                </DropdownMenuItem>
              )}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              {masked ? (
                <DropdownMenuItem onClick={() => run("unmask")}>
                  Unmask
                </DropdownMenuItem>
              ) : (
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => run("mask")}
                >
                  Mask (prevent starting)
                </DropdownMenuItem>
              )}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </ButtonGroup>
      {confirming && (
        <ConfirmAction
          unit={unit}
          name={confirming}
          path={path}
          pending={busy}
          onCancel={() => setConfirming(null)}
          onConfirm={() => action.mutate(confirming)}
        />
      )}
    </>
  )
}

function ConfirmAction({
  unit,
  name,
  path,
  pending,
  onCancel,
  onConfirm,
}: {
  unit: ServiceDetail
  name: ServiceActionName
  path: string
  pending: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  const preview = useMutation<ServiceImpact, Error>({
    mutationFn: () =>
      api<ServiceImpact>(`${path}/actions/preview`, {
        method: "POST",
        body: JSON.stringify({ action: name }),
      }),
  })
  const { mutate } = preview
  React.useEffect(() => mutate(), [mutate])
  const affected = preview.data?.affected ?? []
  return (
    <AlertDialog open onOpenChange={(open) => !open && onCancel()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {labels[name]} {unit.name}?
          </AlertDialogTitle>
          <AlertDialogDescription>
            {name === "mask"
              ? "A masked unit cannot be started, even by other units, until it is unmasked."
              : name === "disable"
                ? "The unit will no longer start automatically at boot. Its current state is unchanged."
                : "Anything that depends on this unit may be interrupted."}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {preview.isPending && <Skeleton className="h-16 w-full" />}
        {preview.isError && (
          <Alert variant="destructive">
            <AlertTitle>Impact preview unavailable</AlertTitle>
            <AlertDescription>{preview.error.message}</AlertDescription>
          </Alert>
        )}
        {preview.data && (
          <div className="flex flex-col gap-2 text-sm">
            {affected.length > 0 ? (
              <>
                <p className="font-medium">Related units ({affected.length})</p>
                <ul className="flex max-h-36 flex-wrap gap-1.5 overflow-auto">
                  {affected.slice(0, 40).map((item) => (
                    <li
                      key={`${item.relationship}-${item.name}`}
                      className="rounded-md bg-muted px-2 py-0.5 font-mono text-xs"
                      title={item.relationship}
                    >
                      {item.name}
                    </li>
                  ))}
                </ul>
              </>
            ) : (
              <p className="text-muted-foreground">
                No other units depend on this one.
              </p>
            )}
            {preview.data.warnings.map((warning) => (
              <p key={warning} className="text-muted-foreground">
                {warning}
              </p>
            ))}
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant={
              name === "stop" || name === "mask" ? "destructive" : "default"
            }
            disabled={pending || !preview.data}
            onClick={(event) => {
              event.preventDefault()
              onConfirm()
            }}
          >
            {pending && <Spinner data-icon="inline-start" />}
            {labels[name]}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
