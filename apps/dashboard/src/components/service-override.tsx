import * as React from "react"
import { useMutation } from "@tanstack/react-query"

import {
  api,
  APIError,
  type ServiceOverrideOperation,
  type ServiceOverrideState,
} from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"

const restartItems = [
  { value: "no", label: "No automatic restart" },
  { value: "on-failure", label: "On failure" },
  { value: "always", label: "Always" },
  { value: "on-abnormal", label: "On abnormal exit" },
]

type OverrideForm = {
  environmentKey: string
  environmentValue: string
  restart: string
  restartSec: string
  memoryMax: string
}

function toOperation(
  scope: string,
  unit: string,
  form: OverrideForm,
  action: "apply" | "delete" | "preview",
  expectedFingerprint?: string
): ServiceOverrideOperation {
  return {
    action,
    scope: scope as "system" | "user",
    unit,
    environment: form.environmentKey
      ? { [form.environmentKey]: form.environmentValue }
      : undefined,
    restart: form.restart || undefined,
    restartSec: form.restartSec || undefined,
    memoryMax: form.memoryMax || undefined,
    expectedFingerprint,
  }
}

function overrideError(error: unknown) {
  if (error instanceof APIError && error.code === "service-override-conflict") {
    return "The drop-in changed elsewhere. Preview again before applying."
  }
  return error instanceof Error ? error.message : "The service override failed."
}

export function ServiceOverride({
  scope,
  unit,
  csrfToken,
  administrative,
}: {
  scope: string
  unit: string
  csrfToken: string
  administrative: boolean
}) {
  const [form, setForm] = React.useState<OverrideForm>({
    environmentKey: "APP_MODE",
    environmentValue: "safe",
    restart: "on-failure",
    restartSec: "5s",
    memoryMax: "",
  })
  const [fingerprint, setFingerprint] = React.useState("")
  const preview = useMutation({
    mutationFn: (operation: ServiceOverrideOperation) =>
      api<ServiceOverrideState>(
        `/services/${scope}/${encodeURIComponent(unit)}/overrides/preview`,
        {
          method: "POST",
          headers: { "X-CSRF-Token": csrfToken },
          body: JSON.stringify(operation),
        }
      ),
    onSuccess: (state) => setFingerprint(state.fingerprint ?? ""),
  })
  const apply = useMutation({
    mutationFn: (operation: ServiceOverrideOperation) =>
      api<ServiceOverrideState>(
        `/services/${scope}/${encodeURIComponent(unit)}/overrides`,
        {
          method: "POST",
          headers: { "X-CSRF-Token": csrfToken },
          body: JSON.stringify(operation),
        }
      ),
    onSuccess: (state) => setFingerprint(state.fingerprint ?? ""),
  })
  const pending = preview.isPending || apply.isPending
  const operation = toOperation(
    scope,
    unit,
    form,
    "apply",
    fingerprint || undefined
  )
  const canWrite = scope === "user" || administrative

  return (
    <Card>
      <CardHeader>
        <CardTitle>Managed override</CardTitle>
        <CardDescription>
          Allowlisted environment and recovery settings in Tako&apos;s
          50-tako.conf drop-in. Vendor content stays read-only.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {preview.isError || apply.isError ? (
          <Alert variant="destructive">
            <AlertTitle>Override unavailable</AlertTitle>
            <AlertDescription>
              {overrideError(preview.error ?? apply.error)}
            </AlertDescription>
          </Alert>
        ) : null}
        {!canWrite && (
          <Alert>
            <AlertTitle>Administrative access required</AlertTitle>
            <AlertDescription>
              Gain temporary administrative access before changing a system
              service.
            </AlertDescription>
          </Alert>
        )}
        <FieldGroup>
          <div className="grid gap-5 @lg/main:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="override-environment-key">
                Environment key
              </FieldLabel>
              <Input
                id="override-environment-key"
                maxLength={128}
                value={form.environmentKey}
                onChange={(event) =>
                  setForm({ ...form, environmentKey: event.target.value })
                }
                disabled={pending || !canWrite}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="override-environment-value">
                Environment value
              </FieldLabel>
              <Input
                id="override-environment-value"
                maxLength={4096}
                value={form.environmentValue}
                onChange={(event) =>
                  setForm({ ...form, environmentValue: event.target.value })
                }
                disabled={pending || !canWrite}
              />
            </Field>
          </div>
          <Field>
            <FieldLabel htmlFor="override-restart">Restart policy</FieldLabel>
            <Select
              items={restartItems}
              value={form.restart}
              onValueChange={(restart) =>
                setForm({ ...form, restart: restart ?? "" })
              }
              disabled={pending || !canWrite}
            >
              <SelectTrigger id="override-restart" aria-label="Restart policy">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {restartItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <div className="grid gap-5 @lg/main:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="override-restart-sec">
                Restart delay
              </FieldLabel>
              <Input
                id="override-restart-sec"
                maxLength={64}
                placeholder="5s"
                value={form.restartSec}
                onChange={(event) =>
                  setForm({ ...form, restartSec: event.target.value })
                }
                disabled={pending || !canWrite}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="override-memory-max">
                Memory limit
              </FieldLabel>
              <Input
                id="override-memory-max"
                maxLength={32}
                placeholder="512M"
                value={form.memoryMax}
                onChange={(event) =>
                  setForm({ ...form, memoryMax: event.target.value })
                }
                disabled={pending || !canWrite}
              />
            </Field>
          </div>
          <FieldDescription>
            Preview detects stale changes. Apply reloads systemd only after the
            drop-in is written atomically.
          </FieldDescription>
        </FieldGroup>
        {preview.data && (
          <Alert>
            <AlertTitle>
              {preview.data.exists ? "Managed drop-in found" : "New drop-in"}
            </AlertTitle>
            <AlertDescription>
              {preview.data.guidance.join(" ")}
              {preview.data.fingerprint &&
                ` Current fingerprint: ${preview.data.fingerprint}`}
            </AlertDescription>
          </Alert>
        )}
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={pending || !canWrite}
            onClick={() =>
              preview.mutate(toOperation(scope, unit, form, "preview"))
            }
          >
            {preview.isPending ? "Previewing…" : "Preview"}
          </Button>
          <Button
            disabled={pending || !canWrite}
            onClick={() => apply.mutate(operation)}
          >
            {apply.isPending ? "Applying…" : "Apply override"}
          </Button>
          <Button
            variant="destructive"
            disabled={pending || !canWrite || !fingerprint}
            onClick={() =>
              apply.mutate({
                action: "delete",
                scope: scope as "system" | "user",
                unit,
                expectedFingerprint: fingerprint,
              })
            }
          >
            Remove drop-in
          </Button>
        </div>
        {pending && <Skeleton className="h-1 w-full" />}
      </CardContent>
    </Card>
  )
}
