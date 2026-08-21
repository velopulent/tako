import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { SendIcon, TriangleAlertIcon } from "lucide-react"

import {
  api,
  type ProcessInfo,
  type SessionResponse,
  type SignalPreview,
  type SignalResult,
} from "@/lib/api"
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
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

const signalOptions = [
  "TERM",
  "KILL",
  "HUP",
  "INT",
  "STOP",
  "CONT",
  "USR1",
  "USR2",
]
const signalItems = signalOptions.map((value) => ({ value, label: value }))

export function ProcessSignal({ process }: { process: ProcessInfo }) {
  const client = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const [signal, setSignal] = React.useState("TERM")
  const [tree, setTree] = React.useState(false)
  const [dialogOpen, setDialogOpen] = React.useState(false)

  const preview = useMutation({
    mutationFn: () =>
      api<SignalPreview>(`/processes/${process.pid}/signal/preview`, {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify({ signal, tree, started: process.started }),
      }),
  })
  const apply = useMutation({
    mutationFn: (value: SignalPreview) =>
      api<SignalResult>(`/processes/${process.pid}/signal`, {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify({
          signal,
          tree,
          started: process.started,
          expectedTargets: value.targets.map(({ pid, started }) => ({
            pid,
            started,
          })),
        }),
      }),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: ["process-details", process.pid] })
      client.invalidateQueries({ queryKey: ["processes"] })
      setDialogOpen(false)
    },
  })

  const handleOpenChange = (open: boolean) => {
    setDialogOpen(open)
    if (!open) {
      // keep preview for retry, but reset apply state when closing
      apply.reset()
    }
  }

  const handleSendClick = () => {
    preview.reset()
    apply.reset()
    setDialogOpen(true)
    preview.mutate()
  }

  const handleConfirm = () => {
    if (preview.data) {
      apply.mutate(preview.data)
    }
  }

  const handleSignalChange = (value: string | null) => {
    setSignal(value ?? "TERM")
    preview.reset()
    apply.reset()
  }

  const handleTreeChange = (value: boolean | "indeterminate") => {
    setTree(value === true)
    preview.reset()
    apply.reset()
  }

  const currentPreview = preview.data
  const error = preview.error ?? apply.error
  const isDangerous = tree || signal === "KILL"

  return (
    <>
      <div className="flex flex-col gap-4 rounded-xl border p-4">
        <div>
          <h4 className="text-sm font-medium">Signal process</h4>
          <p className="text-xs text-muted-foreground">
            Send a signal to this process. Confirmation shows exact targets
            before any signal is delivered.
          </p>
        </div>
        <FieldGroup className="flex flex-col gap-3">
          <Field>
            <FieldLabel htmlFor="process-signal">Signal</FieldLabel>
            <Select
              items={signalItems}
              value={signal}
              onValueChange={handleSignalChange}
            >
              <SelectTrigger id="process-signal" aria-label="Signal">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {signalItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field orientation="horizontal" className="items-center">
            <Checkbox
              id="process-signal-tree"
              checked={tree}
              onCheckedChange={handleTreeChange}
            />
            <FieldLabel htmlFor="process-signal-tree" className="text-sm">
              Include descendants
            </FieldLabel>
          </Field>
        </FieldGroup>

        <Button
          variant={isDangerous ? "destructive" : "default"}
          disabled={!session.data || preview.isPending}
          onClick={handleSendClick}
        >
          <SendIcon data-icon="inline-start" />
          Send {signal}
          {tree ? " to tree" : ""}
        </Button>

        {error && !dialogOpen && (
          <Alert variant="destructive">
            <AlertTitle>Signal action failed</AlertTitle>
            <AlertDescription>{(error as Error).message}</AlertDescription>
          </Alert>
        )}

        {apply.data && (
          <Alert variant={apply.data.failures?.length ? "destructive" : undefined}>
            <AlertTitle>Signal request completed</AlertTitle>
            <AlertDescription>
              Signaled {apply.data.signaled.length} of {apply.data.targets.length}{" "}
              selected processes.
              {apply.data.failures?.length
                ? ` ${apply.data.failures.length} failed.`
                : ""}
            </AlertDescription>
          </Alert>
        )}
      </div>

      <AlertDialog open={dialogOpen} onOpenChange={handleOpenChange}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="flex items-center gap-2">
              {isDangerous ? (
                <TriangleAlertIcon className="size-4 text-destructive" aria-hidden="true" />
              ) : null}
              Confirm signal {signal}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {tree
                ? "This will signal the process and its descendants. Review targets below."
                : "Review the target process before confirming."}
            </AlertDialogDescription>
          </AlertDialogHeader>

          <div className="flex flex-col gap-3 py-2">
            {preview.isPending && (
              <p className="text-sm text-muted-foreground animate-pulse">
                Checking impact — verifying PID start identities…
              </p>
            )}

            {preview.isError && (
              <Alert variant="destructive">
                <AlertTitle>Preview failed</AlertTitle>
                <AlertDescription>{(preview.error as Error).message}</AlertDescription>
              </Alert>
            )}

            {currentPreview && (
              <div className="flex flex-col gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={isDangerous ? "destructive" : "secondary"}>
                    {currentPreview.signal}
                  </Badge>
                  <span className="text-sm">
                    will affect {currentPreview.targets.length} process
                    {currentPreview.targets.length === 1 ? "" : "es"}
                  </span>
                  {tree && <Badge variant="outline">tree</Badge>}
                </div>
                <div className="max-h-48 overflow-auto rounded-lg border bg-muted/30 p-2">
                  <ul className="flex flex-col gap-1.5">
                    {currentPreview.targets.map((target) => (
                      <li
                        key={`${target.pid}:${target.started}`}
                        className="flex flex-wrap items-center gap-2 rounded-md bg-background px-2.5 py-1.5 text-xs shadow-sm"
                      >
                        <span className="font-medium">
                          {target.program}
                        </span>
                        <Badge variant="outline" className="font-mono text-[11px]">
                          PID {target.pid}
                        </Badge>
                        <span className="text-muted-foreground">
                          {target.user}
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
                {isDangerous && (
                  <p className="text-xs text-destructive">
                    This action cannot be undone. {signal === "KILL" ? "KILL cannot be caught or ignored." : "Tree mode affects child processes."}
                  </p>
                )}
              </div>
            )}
          </div>

          <AlertDialogFooter>
            <AlertDialogCancel disabled={apply.isPending}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              disabled={!currentPreview || preview.isPending || apply.isPending}
              onClick={(event) => {
                event.preventDefault()
                handleConfirm()
              }}
              className={isDangerous ? "bg-destructive text-destructive-foreground hover:bg-destructive/90" : undefined}
            >
              {apply.isPending ? "Sending…" : `Confirm and send ${signal}`}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
