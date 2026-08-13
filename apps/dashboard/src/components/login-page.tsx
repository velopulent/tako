import * as React from "react"
import {
  LoaderCircleIcon,
  LockKeyholeIcon,
  ShieldCheckIcon,
  UserIcon,
} from "lucide-react"

import { APIError, api, type SessionResponse } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
} from "@/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group"

export function LoginPage({
  onAuthenticated,
}: {
  onAuthenticated: (session: SessionResponse) => void
}) {
  const [error, setError] = React.useState("")
  const [pending, startTransition] = React.useTransition()

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    setError("")
    startTransition(async () => {
      try {
        const session = await api<SessionResponse>("/auth/login", {
          method: "POST",
          body: JSON.stringify({
            username: String(form.get("username") ?? ""),
            password: String(form.get("password") ?? ""),
          }),
        })
        onAuthenticated(session)
      } catch (caught) {
        if (
          caught instanceof APIError &&
          caught.code === "authentication-unavailable"
        ) {
          setError(
            "Authentication service is unavailable. Start tako sessiond, then try again."
          )
        } else {
          setError("Check your username and password, then try again.")
        }
      }
    })
  }

  return (
    <main className="grid min-h-svh place-items-center bg-muted p-4 sm:p-8">
      <Card className="w-full max-w-sm shadow-xl">
        <CardHeader>
          <div className="mb-4 flex size-11 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <ShieldCheckIcon aria-hidden="true" />
          </div>
          <CardTitle className="text-2xl">Welcome to Tako</CardTitle>
          <CardDescription>
            Sign in with your Linux account to manage this host.
          </CardDescription>
        </CardHeader>
        <form onSubmit={submit}>
          <CardContent>
            <FieldGroup>
              {error !== "" && (
                <Alert variant="destructive">
                  <AlertTitle>Sign-in failed</AlertTitle>
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
              <Field>
                <FieldLabel htmlFor="username">Username</FieldLabel>
                <InputGroup>
                  <InputGroupAddon>
                    <UserIcon aria-hidden="true" />
                  </InputGroupAddon>
                  <InputGroupInput
                    id="username"
                    name="username"
                    autoComplete="username"
                    required
                    autoFocus
                  />
                </InputGroup>
              </Field>
              <Field>
                <FieldLabel htmlFor="password">Password</FieldLabel>
                <InputGroup>
                  <InputGroupAddon>
                    <LockKeyholeIcon aria-hidden="true" />
                  </InputGroupAddon>
                  <InputGroupInput
                    id="password"
                    name="password"
                    type="password"
                    autoComplete="current-password"
                  />
                </InputGroup>
                <FieldDescription>
                  Authentication follows this host&apos;s PAM policy.
                </FieldDescription>
              </Field>
            </FieldGroup>
          </CardContent>
          <CardFooter className="mt-6 flex-col gap-3">
            <Button className="w-full" type="submit" disabled={pending}>
              {pending && (
                <LoaderCircleIcon
                  data-icon="inline-start"
                  className="animate-spin"
                />
              )}
              {pending ? "Signing in…" : "Sign in"}
            </Button>
            <p className="text-center text-xs text-muted-foreground">
              Credentials are passed to PAM and never stored by Tako.
            </p>
          </CardFooter>
        </form>
      </Card>
    </main>
  )
}
