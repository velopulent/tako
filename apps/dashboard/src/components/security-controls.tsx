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
  type SecurityOperation,
  type SecurityStatus,
  type SessionResponse,
} from "@/lib/api"

export function SecurityControls() {
  const queryClient = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const status = useQuery({
    queryKey: ["security"],
    queryFn: () => api<SecurityStatus>("/security"),
  })
  const [framework, setFramework] =
    React.useState<SecurityOperation["framework"]>("SELinux")
  const [action, setAction] =
    React.useState<SecurityOperation["action"]>("selinux-boolean")
  const [booleanName, setBooleanName] = React.useState("")
  const [booleanValue, setBooleanValue] = React.useState(false)
  const [profile, setProfile] = React.useState("")
  const [path, setPath] = React.useState("")
  const [confirmation, setConfirmation] = React.useState("")
  const [previewKey, setPreviewKey] = React.useState("")
  const operation = React.useCallback(
    (expectedFingerprint = status.data?.fingerprint): SecurityOperation => ({
      action,
      framework,
      boolean:
        action === "selinux-boolean" ? booleanName || undefined : undefined,
      value: action === "selinux-boolean" ? booleanValue : undefined,
      path:
        action === "selinux-restorecon" || action === "apparmor-load"
          ? path || undefined
          : undefined,
      profile:
        action === "apparmor-enforce" || action === "apparmor-complain"
          ? profile || undefined
          : undefined,
      expectedFingerprint,
      confirmation: confirmation || undefined,
    }),
    [
      action,
      booleanName,
      booleanValue,
      confirmation,
      framework,
      path,
      profile,
      status.data?.fingerprint,
    ]
  )
  const currentKey = JSON.stringify(operation())
  const preview = useMutation({
    mutationFn: () =>
      api<SecurityStatus>("/security/preview", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation()),
      }),
    onSuccess: (value) =>
      setPreviewKey(JSON.stringify(operation(value.fingerprint))),
  })
  const apply = useMutation({
    mutationFn: () =>
      api<SecurityStatus>("/security", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation()),
      }),
    onSuccess: () => {
      setPreviewKey("")
      void queryClient.invalidateQueries({ queryKey: ["security"] })
    },
  })
  const previewReady =
    Boolean(preview.data?.allowed) &&
    !preview.data?.stale &&
    previewKey === currentKey
  const actionOptions: SecurityOperation["action"][] =
    framework === "SELinux"
      ? ["selinux-boolean", "selinux-restorecon"]
      : ["apparmor-enforce", "apparmor-complain", "apparmor-load"]
  const invalidInput =
    (action === "selinux-boolean" && !booleanName) ||
    (action === "selinux-restorecon" && !path) ||
    ((action === "apparmor-enforce" || action === "apparmor-complain") &&
      !profile) ||
    (action === "apparmor-load" && !path)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Security policy</CardTitle>
        <CardDescription>
          Inspect active policy frameworks, preview one narrow remediation, then
          apply only the exact fingerprinted change that was reviewed.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {status.isPending && (
          <div className="h-20 animate-pulse rounded bg-muted" />
        )}
        {status.isError && (
          <Alert variant="destructive">
            <AlertTitle>Policy status unavailable</AlertTitle>
            <AlertDescription>
              Administrative access or policy tooling is unavailable.
            </AlertDescription>
          </Alert>
        )}
        {status.data && (
          <>
            <div className="flex flex-wrap gap-2">
              <Badge variant="outline">
                Active: {status.data.active || "none"}
              </Badge>
              <Badge variant="outline">
                SELinux: {status.data.selinux.mode || "unknown"}
              </Badge>
              <Badge variant="outline">
                AppArmor profiles: {status.data.apparmor.profiles.length}
              </Badge>
            </div>
            {status.data.findings.length > 0 && (
              <div className="space-y-2">
                {status.data.findings.map((finding) => (
                  <Alert key={`${finding.framework}:${finding.subject}`}>
                    <AlertTitle>
                      {finding.framework} · {finding.severity}
                    </AlertTitle>
                    <AlertDescription>
                      {finding.message} {finding.guidance}
                    </AlertDescription>
                  </Alert>
                ))}
              </div>
            )}
          </>
        )}
        <FieldGroup className="grid gap-4 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="security-framework">Framework</FieldLabel>
            <select
              id="security-framework"
              className="h-9 rounded-md border bg-background px-3 text-sm"
              value={framework}
              onChange={(event) => {
                const next = event.target
                  .value as SecurityOperation["framework"]
                setFramework(next)
                setAction(
                  next === "SELinux" ? "selinux-boolean" : "apparmor-enforce"
                )
                setPreviewKey("")
              }}
            >
              <option value="SELinux">SELinux</option>
              <option value="AppArmor">AppArmor</option>
            </select>
          </Field>
          <Field>
            <FieldLabel htmlFor="security-action">Remediation</FieldLabel>
            <select
              id="security-action"
              className="h-9 rounded-md border bg-background px-3 text-sm"
              value={action}
              onChange={(event) => {
                setAction(event.target.value as SecurityOperation["action"])
                setPreviewKey("")
              }}
            >
              {actionOptions.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </Field>
          {action === "selinux-boolean" && (
            <>
              <Field>
                <FieldLabel htmlFor="security-boolean">
                  SELinux boolean
                </FieldLabel>
                <Input
                  id="security-boolean"
                  value={booleanName}
                  onChange={(event) => {
                    setBooleanName(event.target.value)
                    setPreviewKey("")
                  }}
                  placeholder="httpd_can_network_connect"
                />
                <FieldDescription>
                  Must be present in the reported boolean inventory.
                </FieldDescription>
              </Field>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={booleanValue}
                  onChange={(event) => {
                    setBooleanValue(event.target.checked)
                    setPreviewKey("")
                  }}
                />
                Enable boolean
              </label>
            </>
          )}
          {action === "selinux-restorecon" && (
            <Field>
              <FieldLabel htmlFor="security-path">Path</FieldLabel>
              <Input
                id="security-path"
                value={path}
                onChange={(event) => {
                  setPath(event.target.value)
                  setPreviewKey("")
                }}
                placeholder="/srv/app"
              />
            </Field>
          )}
          {(action === "apparmor-enforce" ||
            action === "apparmor-complain") && (
            <Field>
              <FieldLabel htmlFor="security-profile">
                AppArmor profile
              </FieldLabel>
              <select
                id="security-profile"
                className="h-9 rounded-md border bg-background px-3 text-sm"
                value={profile}
                onChange={(event) => {
                  setProfile(event.target.value)
                  setPreviewKey("")
                }}
              >
                <option value="">Select a reported profile</option>
                {(status.data?.apparmor.profiles ?? []).map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </Field>
          )}
          {action === "apparmor-load" && (
            <Field>
              <FieldLabel htmlFor="security-profile-path">
                Profile path
              </FieldLabel>
              <Input
                id="security-profile-path"
                value={path}
                onChange={(event) => {
                  setPath(event.target.value)
                  setPreviewKey("")
                }}
                placeholder="/etc/apparmor.d/usr.sbin.example"
              />
            </Field>
          )}
          <Field>
            <FieldLabel htmlFor="security-confirmation">
              Confirmation
            </FieldLabel>
            <Input
              id="security-confirmation"
              value={confirmation}
              onChange={(event) => {
                setConfirmation(event.target.value)
                setPreviewKey("")
              }}
              placeholder="CONFIRM NARROW SECURITY CHANGE"
            />
          </Field>
        </FieldGroup>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            onClick={() => preview.mutate()}
            disabled={
              invalidInput || !confirmation || preview.isPending || !status.data
            }
          >
            Preview remediation
          </Button>
          <Button
            onClick={() => apply.mutate()}
            disabled={!previewReady || apply.isPending}
          >
            Apply reviewed change
          </Button>
        </div>
        {preview.data && (
          <Alert
            variant={
              preview.data.allowed && !preview.data.stale
                ? "default"
                : "destructive"
            }
          >
            <AlertTitle>
              {preview.data.stale ? "Preview is stale" : "Preview ready"}
            </AlertTitle>
            <AlertDescription className="space-y-1">
              {(preview.data.changes ?? []).map((change) => (
                <span key={change.field} className="block">
                  {change.field}: {change.before ?? "unknown"} →{" "}
                  {change.after ?? "no change"}
                </span>
              ))}
              {(preview.data.warnings ?? []).map((warning) => (
                <span key={warning} className="block text-muted-foreground">
                  {warning}
                </span>
              ))}
            </AlertDescription>
          </Alert>
        )}
        {(preview.isError || apply.isError) && (
          <Alert variant="destructive">
            <AlertTitle>Remediation rejected</AlertTitle>
            <AlertDescription>
              The policy fingerprint, authority, path, or allowlisted operation
              did not pass validation.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}
