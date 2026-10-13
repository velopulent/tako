import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { PlusIcon, Trash2Icon } from "lucide-react"
import * as React from "react"

import { AdminRequired, useAdminAccess } from "@/components/admin-access"
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
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import {
  APIError,
  api,
  type ServiceOverrideOperation,
  type ServiceOverrideState,
} from "@/lib/api"
import { QueryError } from "@/lib/page"

const restartItems = [
  { value: "", label: "Keep unit default" },
  { value: "no", label: "Never" },
  { value: "on-failure", label: "On failure" },
  { value: "on-abnormal", label: "On abnormal exit" },
  { value: "always", label: "Always" },
]

type Variable = { id: number; key: string; value: string }

type Form = {
  variables: Variable[]
  restart: string
  restartSec: string
  memoryMax: string
  cpuQuota: string
  tasksMax: string
}

let nextVariable = 0

function fromState(state: ServiceOverrideState | undefined): Form {
  const definition = state?.definition
  return {
    variables: Object.entries(definition?.environment ?? {}).map(
      ([key, value]) => ({ id: nextVariable++, key, value })
    ),
    restart: definition?.restart ?? "",
    restartSec: definition?.restartSec ?? "",
    memoryMax: definition?.memoryMax ?? "",
    cpuQuota: definition?.cpuQuota ?? "",
    tasksMax: definition?.tasksMax ? String(definition.tasksMax) : "",
  }
}

function toOperation(
  scope: "system" | "user",
  unit: string,
  form: Form,
  fingerprint?: string
): ServiceOverrideOperation {
  const environment = Object.fromEntries(
    form.variables
      .filter((item) => item.key.trim())
      .map((item) => [item.key.trim(), item.value])
  )
  return {
    action: "apply",
    scope,
    unit,
    environment: Object.keys(environment).length ? environment : undefined,
    restart: form.restart || undefined,
    restartSec: form.restartSec.trim() || undefined,
    memoryMax: form.memoryMax.trim() || undefined,
    cpuQuota: form.cpuQuota.trim() || undefined,
    tasksMax: form.tasksMax.trim() ? Number(form.tasksMax) : undefined,
    expectedFingerprint: fingerprint,
  }
}

function errorMessage(error: unknown) {
  if (error instanceof APIError && error.code === "service-override-conflict")
    return "The override changed elsewhere. Reload it and try again."
  if (error instanceof APIError && error.code === "invalid-service-override")
    return "One of the values is not valid for systemd. Check durations (5s), sizes (512M), and percentages (50%)."
  return error instanceof Error
    ? error.message
    : "The override could not be saved."
}

// ServiceOverride edits Tako's managed drop-in (50-tako.conf). Only an
// allowlisted set of settings is accepted; the vendor unit stays untouched.
export function ServiceOverride({
  scope,
  unit,
}: {
  scope: "system" | "user"
  unit: string
}) {
  const client = useQueryClient()
  const { administrative } = useAdminAccess()
  const canWrite = scope === "user" || administrative
  const key = ["service-override", scope, unit]
  const state = useQuery({
    queryKey: key,
    queryFn: () =>
      api<ServiceOverrideState>(
        `/services/${scope}/${encodeURIComponent(unit)}/overrides/preview`,
        {
          method: "POST",
          body: JSON.stringify({ action: "preview", scope, unit }),
        }
      ),
    enabled: canWrite,
  })
  const [form, setForm] = React.useState<Form>(() => fromState(undefined))
  React.useEffect(() => setForm(fromState(state.data)), [state.data])

  const save = useMutation({
    mutationFn: (operation: ServiceOverrideOperation) =>
      api<ServiceOverrideState>(
        `/services/${scope}/${encodeURIComponent(unit)}/overrides`,
        { method: "POST", body: JSON.stringify(operation) }
      ),
    onSuccess: (next, operation) => {
      client.setQueryData(key, next)
      toast.add({
        title:
          operation.action === "delete" ? "Override removed" : "Override saved",
        description:
          "systemd reloaded its configuration. Restart the service to apply it.",
        type: "success",
      })
    },
  })

  if (!canWrite) {
    return (
      <AdminRequired>
        Overriding a system service requires administrative access.
      </AdminRequired>
    )
  }
  if (state.isPending) return <Skeleton className="h-96" />
  if (state.isError)
    return <QueryError error={state.error} retry={() => void state.refetch()} />

  const exists = state.data?.exists ?? false
  const fingerprint = state.data?.fingerprint
  const update = (patch: Partial<Form>) =>
    setForm((current) => ({ ...current, ...patch }))
  const updateVariable = (id: number, patch: Partial<Variable>) =>
    update({
      variables: form.variables.map((item) =>
        item.id === id ? { ...item, ...patch } : item
      ),
    })
  const pending = save.isPending

  return (
    <Card>
      <CardHeader>
        <CardTitle>Override settings</CardTitle>
        <CardDescription>
          Stored in a Tako-managed drop-in
          {state.data?.path ? (
            <span className="font-mono"> {state.data.path}</span>
          ) : null}
          . The vendor unit file is never edited.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          id="service-override"
          onSubmit={(event) => {
            event.preventDefault()
            save.mutate(
              toOperation(scope, unit, form, exists ? fingerprint : undefined)
            )
          }}
        >
          <FieldGroup>
            {save.isError && (
              <Alert variant="destructive">
                <AlertTitle>Override not saved</AlertTitle>
                <AlertDescription>{errorMessage(save.error)}</AlertDescription>
              </Alert>
            )}
            <FieldSet>
              <FieldLegend variant="label">Environment variables</FieldLegend>
              <FieldDescription>
                Passed to the service in addition to its own environment.
              </FieldDescription>
              <div className="flex flex-col gap-2">
                {form.variables.map((item) => (
                  <div
                    key={item.id}
                    className="grid grid-cols-[1fr_1.5fr_auto] gap-2"
                  >
                    <Input
                      aria-label="Variable name"
                      placeholder="NAME"
                      className="font-mono"
                      maxLength={128}
                      value={item.key}
                      onChange={(event) =>
                        updateVariable(item.id, { key: event.target.value })
                      }
                    />
                    <Input
                      aria-label="Variable value"
                      placeholder="value"
                      className="font-mono"
                      maxLength={4096}
                      value={item.value}
                      onChange={(event) =>
                        updateVariable(item.id, { value: event.target.value })
                      }
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={`Remove ${item.key || "variable"}`}
                      onClick={() =>
                        update({
                          variables: form.variables.filter(
                            (other) => other.id !== item.id
                          ),
                        })
                      }
                    >
                      <Trash2Icon />
                    </Button>
                  </div>
                ))}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="self-start"
                  onClick={() =>
                    update({
                      variables: [
                        ...form.variables,
                        { id: nextVariable++, key: "", value: "" },
                      ],
                    })
                  }
                >
                  <PlusIcon data-icon="inline-start" />
                  Add variable
                </Button>
              </div>
            </FieldSet>
            <div className="grid gap-4 @2xl/main:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="override-restart">
                  Restart automatically
                </FieldLabel>
                <Select
                  items={restartItems}
                  value={form.restart}
                  onValueChange={(restart) =>
                    update({ restart: String(restart ?? "") })
                  }
                >
                  <SelectTrigger id="override-restart">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {restartItems.map((item) => (
                        <SelectItem
                          key={item.value || "default"}
                          value={item.value}
                        >
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="override-restart-sec">
                  Restart delay
                </FieldLabel>
                <Input
                  id="override-restart-sec"
                  placeholder="e.g. 5s"
                  maxLength={64}
                  value={form.restartSec}
                  onChange={(event) =>
                    update({ restartSec: event.target.value })
                  }
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="override-memory">Memory limit</FieldLabel>
                <Input
                  id="override-memory"
                  placeholder="e.g. 512M"
                  maxLength={32}
                  value={form.memoryMax}
                  onChange={(event) =>
                    update({ memoryMax: event.target.value })
                  }
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="override-cpu">CPU quota</FieldLabel>
                <Input
                  id="override-cpu"
                  placeholder="e.g. 50%"
                  maxLength={16}
                  value={form.cpuQuota}
                  onChange={(event) => update({ cpuQuota: event.target.value })}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="override-tasks">Maximum tasks</FieldLabel>
                <Input
                  id="override-tasks"
                  inputMode="numeric"
                  placeholder="e.g. 512"
                  maxLength={9}
                  value={form.tasksMax}
                  onChange={(event) =>
                    update({ tasksMax: event.target.value.replace(/\D/g, "") })
                  }
                />
              </Field>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
      <CardFooter className="flex flex-wrap justify-between gap-2 border-t">
        {exists ? (
          <AlertDialog>
            <AlertDialogTrigger
              render={
                <Button
                  variant="ghost"
                  className="text-destructive"
                  disabled={pending}
                />
              }
            >
              Remove override
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Remove the override?</AlertDialogTitle>
                <AlertDialogDescription>
                  {unit} returns to its vendor settings after its next restart.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancel</AlertDialogCancel>
                <AlertDialogAction
                  variant="destructive"
                  onClick={() =>
                    save.mutate({
                      action: "delete",
                      scope,
                      unit,
                      expectedFingerprint: fingerprint,
                    })
                  }
                >
                  Remove
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        ) : (
          <span className="text-sm text-muted-foreground">
            No override yet.
          </span>
        )}
        <div className="flex gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            onClick={() => setForm(fromState(state.data))}
          >
            Reset
          </Button>
          <Button type="submit" form="service-override" disabled={pending}>
            {pending && <Spinner data-icon="inline-start" />}
            Save override
          </Button>
        </div>
      </CardFooter>
    </Card>
  )
}
