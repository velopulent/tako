import type { TimerAction, TimerOperation } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
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
import { Textarea } from "@/components/ui/textarea"

const scopeItems = [
  { value: "user", label: "User session" },
  { value: "system", label: "System" },
]
const actionItems = [
  { value: "create", label: "Create" },
  { value: "update", label: "Update" },
  { value: "enable", label: "Enable" },
  { value: "disable", label: "Disable" },
  { value: "delete", label: "Delete" },
]
const scheduleItems = [
  { value: "calendar", label: "OnCalendar" },
  { value: "boot", label: "OnBootSec" },
  { value: "active", label: "OnUnitActiveSec" },
] as const

type ScheduleKind = (typeof scheduleItems)[number]["value"]

function scheduleKind(value: TimerOperation): ScheduleKind {
  if (value.onBootSec) return "boot"
  if (value.onUnitActiveSec) return "active"
  return "calendar"
}

function updateSchedule(
  value: TimerOperation,
  kind: ScheduleKind,
  schedule: string
): TimerOperation {
  return {
    ...value,
    onCalendar: kind === "calendar" ? schedule : undefined,
    onBootSec: kind === "boot" ? schedule : undefined,
    onUnitActiveSec: kind === "active" ? schedule : undefined,
  }
}

export function TimerForm({
  value,
  onChange,
  onSubmit,
  disabled = false,
  submitLabel = "Apply timer",
}: {
  value: TimerOperation
  onChange: (value: TimerOperation) => void
  onSubmit: () => void
  disabled?: boolean
  submitLabel?: string
}) {
  const kind = scheduleKind(value)
  const schedule =
    value.onCalendar ?? value.onBootSec ?? value.onUnitActiveSec ?? ""
  const requiresFingerprint = [
    "update",
    "delete",
    "enable",
    "disable",
  ].includes(value.action)

  const set = <K extends keyof TimerOperation>(
    key: K,
    next: TimerOperation[K]
  ) => onChange({ ...value, [key]: next })

  return (
    <form
      className="space-y-6"
      onSubmit={(event) => {
        event.preventDefault()
        onSubmit()
      }}
    >
      <FieldGroup>
        <div className="grid gap-5 @lg/main:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="timer-scope">Scope</FieldLabel>
            <Select
              items={scopeItems}
              value={value.scope}
              onValueChange={(scope) =>
                set("scope", scope as TimerOperation["scope"])
              }
              disabled={disabled}
            >
              <SelectTrigger id="timer-scope" aria-label="Timer scope">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {scopeItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <FieldDescription>
              User timers run as the signed-in account; system timers require
              administrative access.
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="timer-action">Operation</FieldLabel>
            <Select
              items={actionItems}
              value={value.action}
              onValueChange={(action) => set("action", action as TimerAction)}
              disabled={disabled}
            >
              <SelectTrigger id="timer-action" aria-label="Timer operation">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {actionItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
        </div>
        <Field>
          <FieldLabel htmlFor="timer-name">Timer name</FieldLabel>
          <Input
            id="timer-name"
            required
            maxLength={64}
            pattern="[A-Za-z0-9@._-]+"
            value={value.name}
            onChange={(event) => set("name", event.target.value)}
            disabled={disabled}
          />
          <FieldDescription>
            A unit stem only. Tako creates the matching .timer and .service
            pair.
          </FieldDescription>
        </Field>
        <Field>
          <FieldLabel htmlFor="timer-description">Description</FieldLabel>
          <Input
            id="timer-description"
            maxLength={256}
            value={value.description ?? ""}
            onChange={(event) => set("description", event.target.value)}
            disabled={disabled}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="timer-schedule-type">Schedule</FieldLabel>
          <Select
            items={scheduleItems}
            value={kind}
            onValueChange={(next) =>
              onChange(updateSchedule(value, next as ScheduleKind, schedule))
            }
            disabled={disabled}
          >
            <SelectTrigger id="timer-schedule-type" aria-label="Schedule type">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {scheduleItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <Input
            aria-label={`${kind} schedule`}
            maxLength={256}
            placeholder={kind === "calendar" ? "*-*-* 03:00:00" : "1h"}
            value={schedule}
            onChange={(event) =>
              onChange(updateSchedule(value, kind, event.target.value))
            }
            disabled={disabled}
          />
          <FieldDescription>
            Exactly one schedule is written to the timer unit.
          </FieldDescription>
        </Field>
        <Field>
          <FieldLabel htmlFor="timer-command">Command</FieldLabel>
          <Textarea
            id="timer-command"
            required
            maxLength={512}
            rows={2}
            placeholder="/usr/local/bin/backup --all"
            value={value.command ?? ""}
            onChange={(event) => set("command", event.target.value)}
            disabled={disabled}
          />
          <FieldDescription>
            Absolute executable path and arguments. Shell metacharacters are
            rejected.
          </FieldDescription>
        </Field>
        <Field orientation="horizontal">
          <Checkbox
            id="timer-persistent"
            checked={value.persistent ?? false}
            onCheckedChange={(checked) => set("persistent", checked === true)}
            disabled={disabled}
          />
          <FieldLabel htmlFor="timer-persistent">
            Persistent after downtime
          </FieldLabel>
        </Field>
        {requiresFingerprint && (
          <Field>
            <FieldLabel htmlFor="timer-fingerprint">
              Expected fingerprint
            </FieldLabel>
            <Input
              id="timer-fingerprint"
              required
              minLength={64}
              maxLength={64}
              pattern="[a-fA-F0-9]{64}"
              value={value.expectedFingerprint ?? ""}
              onChange={(event) =>
                set("expectedFingerprint", event.target.value)
              }
              disabled={disabled}
            />
            <FieldDescription>
              Stale writes are rejected when another browser changed this timer.
            </FieldDescription>
          </Field>
        )}
      </FieldGroup>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={disabled}>
          {submitLabel}
        </Button>
      </div>
    </form>
  )
}
