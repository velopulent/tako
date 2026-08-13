import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import {
  api,
  type ProcessInfo,
  type SessionResponse,
  type SignalPreview,
  type SignalResult,
} from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
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

export function ProcessSignal({ process }: { process: ProcessInfo }) {
  const client = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const [signal, setSignal] = React.useState("TERM")
  const [tree, setTree] = React.useState(false)
  const [confirmation, setConfirmation] = React.useState("")
  const preview = useMutation({
    mutationFn: () =>
      api<SignalPreview>(`/processes/${process.pid}/signal/preview`, {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify({ signal, tree, started: process.started }),
      }),
    onSuccess: () => setConfirmation(""),
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
    },
  })
  const currentPreview = preview.data
  const needsTypedConfirmation = tree || signal === "KILL"
  const error = preview.error ?? apply.error
  return (
    <div className="space-y-3 rounded-lg border p-3">
      <div>
        <h4 className="font-medium">Signal process</h4>
        <p className="text-sm text-muted-foreground">
          Preview verifies PID start identities before the session service
          signals anything.
        </p>
      </div>
      <FieldGroup className="grid gap-3 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="process-signal">Signal</FieldLabel>
          <Select
            value={signal}
            onValueChange={(value) => {
              setSignal(value ?? "TERM")
              preview.reset()
              apply.reset()
            }}
          >
            <SelectTrigger id="process-signal" aria-label="Signal">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {signalOptions.map((value) => (
                <SelectItem key={value} value={value}>
                  {value}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field orientation="horizontal" className="items-center pt-6">
          <Checkbox
            id="process-signal-tree"
            checked={tree}
            onCheckedChange={(value) => {
              setTree(value === true)
              preview.reset()
              apply.reset()
            }}
          />
          <FieldLabel htmlFor="process-signal-tree">
            Include descendants
          </FieldLabel>
        </Field>
      </FieldGroup>
      <Button
        variant="outline"
        disabled={preview.isPending || !session.data}
        onClick={() => preview.mutate()}
      >
        Preview impact
      </Button>
      {error && (
        <Alert variant="destructive">
          <AlertTitle>Signal action failed</AlertTitle>
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
      )}
      {currentPreview && (
        <Alert>
          <AlertTitle>
            {currentPreview.signal} will affect {currentPreview.targets.length}{" "}
            process
            {currentPreview.targets.length === 1 ? "" : "es"}
          </AlertTitle>
          <AlertDescription>
            <ul className="mt-1 list-disc pl-5">
              {currentPreview.targets.map((target) => (
                <li key={`${target.pid}:${target.started}`}>
                  {target.program} (PID {target.pid}, {target.user})
                </li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}
      {currentPreview && needsTypedConfirmation && (
        <Field>
          <FieldLabel htmlFor="process-signal-confirm">
            Type CONFIRM to continue
          </FieldLabel>
          <Input
            id="process-signal-confirm"
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
            placeholder="CONFIRM"
          />
        </Field>
      )}
      {currentPreview && (
        <Button
          variant="destructive"
          disabled={
            apply.isPending ||
            (needsTypedConfirmation && confirmation !== "CONFIRM")
          }
          onClick={() => apply.mutate(currentPreview)}
        >
          Send {currentPreview.signal}
        </Button>
      )}
      {apply.data && (
        <Alert
          variant={apply.data.failures?.length ? "destructive" : undefined}
        >
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
  )
}
