import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api, type SecurityStatus, type SessionResponse } from "@/lib/api"
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
  const [framework, setFramework] = React.useState<"SELinux" | "AppArmor">(
    "SELinux"
  )
  const [booleanName, setBooleanName] = React.useState("")
  const [profile, setProfile] = React.useState("")
  const [confirmation, setConfirmation] = React.useState("")
  const operation = (action: string) => ({
    action,
    framework,
    boolean: booleanName || undefined,
    profile: profile || undefined,
    expectedFingerprint: status.data?.fingerprint,
    confirmation,
  })
  const preview = useMutation({
    mutationFn: () =>
      api<SecurityStatus>("/security/preview", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify({ action: "inspect", framework }),
      }),
  })
  const apply = useMutation({
    mutationFn: (action: string) =>
      api<SecurityStatus>("/security", {
        method: "POST",
        headers: { "X-CSRF-Token": session.data?.csrfToken ?? "" },
        body: JSON.stringify(operation(action)),
      }),
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: ["security"] }),
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>Security policy</CardTitle>
        <CardDescription>
          Inspect active policy frameworks and apply one narrow,
          fingerprint-checked remediation at a time.
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
              onChange={(event) =>
                setFramework(event.target.value as "SELinux" | "AppArmor")
              }
            >
              <option>SELinux</option>
              <option>AppArmor</option>
            </select>
            <FieldDescription>
              Capabilities are reported independently.
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="security-boolean">SELinux boolean</FieldLabel>
            <Input
              id="security-boolean"
              value={booleanName}
              onChange={(event) => setBooleanName(event.target.value)}
              placeholder="httpd_can_network_connect"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="security-profile">AppArmor profile</FieldLabel>
            <Input
              id="security-profile"
              value={profile}
              onChange={(event) => setProfile(event.target.value)}
              placeholder="usr.sbin.example"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="security-confirmation">
              Confirmation
            </FieldLabel>
            <Input
              id="security-confirmation"
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              placeholder="CONFIRM NARROW SECURITY CHANGE"
            />
          </Field>
        </FieldGroup>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            onClick={() => preview.mutate()}
            disabled={preview.isPending}
          >
            Refresh policy
          </Button>
          <Button
            variant="outline"
            onClick={() =>
              apply.mutate(
                framework === "SELinux" ? "selinux-boolean" : "apparmor-enforce"
              )
            }
            disabled={apply.isPending || !status.data}
          >
            Apply narrow change
          </Button>
        </div>
        {(preview.isError || apply.isError) && (
          <Alert variant="destructive">
            <AlertTitle>Remediation rejected</AlertTitle>
            <AlertDescription>
              The policy fingerprint, authority, or allowlisted operation did
              not pass validation.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}
