import {
  LoaderCircleIcon,
  LockKeyholeIcon,
  ShieldCheckIcon,
  UserIcon,
} from "lucide-react"
import * as React from "react"
import { LoginPromptFields } from "@/components/login-prompt-fields"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
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
import {
  APIError,
  api,
  type BrandingResponse,
  type LoginChallenge,
  type SessionResponse,
} from "@/lib/api"
import { cn } from "@/lib/utils"

const loginInputGroupClass =
  "h-12 rounded-xl border-foreground/15 bg-foreground/5 shadow-sm transition-colors focus-within:border-primary/60 focus-within:bg-foreground/10"

function LoginBackdrop({
  branding,
  children,
}: {
  branding?: BrandingResponse
  children: React.ReactNode
}) {
  const backgroundUrl = branding?.backgroundUrl
  const [loadedBackground, setLoadedBackground] = React.useState("")
  const imageVisible =
    loadedBackground === backgroundUrl && backgroundUrl !== undefined

  return (
    <main className="dark relative isolate flex min-h-svh items-center justify-center overflow-y-auto bg-background px-4 py-8 text-foreground sm:px-8 sm:py-12">
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 overflow-hidden"
      >
        <div className="absolute inset-0 bg-background" />
        {backgroundUrl ? (
          <img
            alt=""
            aria-hidden="true"
            className={cn(
              "absolute inset-0 size-full object-cover object-center transition-opacity duration-1000 ease-out motion-reduce:transition-none",
              imageVisible ? "opacity-100" : "opacity-0"
            )}
            decoding="async"
            fetchPriority="high"
            onError={() => setLoadedBackground("")}
            onLoad={() => setLoadedBackground(backgroundUrl)}
            src={backgroundUrl}
          />
        ) : null}
        <div className="absolute -top-40 -left-40 size-[28rem] rounded-full bg-primary/10 blur-3xl" />
        <div className="absolute -right-40 -bottom-40 size-[30rem] rounded-full bg-accent/10 blur-3xl" />
        <div className="absolute inset-0 bg-background/35" />
        <div className="absolute inset-0 bg-gradient-to-br from-background/90 via-background/15 to-background/75" />
      </div>
      <div className="relative z-10 flex w-full max-w-md items-center justify-center">
        {children}
      </div>
    </main>
  )
}

export function LoginPage({
  onAuthenticated,
  branding,
}: {
  onAuthenticated: (session: SessionResponse) => void
  branding?: BrandingResponse
}) {
  const [error, setError] = React.useState("")
  const [challenge, setChallenge] = React.useState<LoginChallenge | null>(null)
  const [pending, startTransition] = React.useTransition()
  const hostname = branding?.hostname?.trim() || "Tako"

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    setError("")
    startTransition(async () => {
      try {
        const result = await api<SessionResponse | LoginChallenge>(
          "/auth/login",
          {
            method: "POST",
            body: JSON.stringify(
              challenge
                ? {
                    conversationId: challenge.conversationId,
                    responses: challenge.prompts.map((prompt) => ({
                      id: prompt.id,
                      value: String(form.get(prompt.id) ?? ""),
                    })),
                  }
                : {
                    username: String(form.get("username") ?? ""),
                    password: String(form.get("password") ?? ""),
                  }
            ),
          }
        )
        if ("prompts" in result) {
          setChallenge(result)
        } else {
          onAuthenticated(result)
        }
      } catch (caught) {
        setChallenge(null)
        if (
          caught instanceof APIError &&
          caught.code === "authentication-unavailable"
        ) {
          setError(
            "Authentication service is unavailable. Start tako sessiond, then try again."
          )
        } else if (
          caught instanceof APIError &&
          caught.code === "session-failed"
        ) {
          setError(
            "Signed in, but the user session could not start. Check tako-sessiond on the host."
          )
        } else {
          setError(
            challenge
              ? "The authentication response was rejected. Start again."
              : "Check your username and password, then try again."
          )
        }
      }
    })
  }

  function cancelConversation() {
    const conversationId = challenge?.conversationId
    setChallenge(null)
    setError("")
    if (conversationId) {
      void api<void>("/auth/login", {
        method: "POST",
        body: JSON.stringify({ conversationId, cancel: true }),
      }).catch(() => {})
    }
  }

  return (
    <LoginBackdrop branding={branding}>
      <Card className="w-full max-w-md border border-foreground/15 bg-card/85 py-0 text-card-foreground shadow-2xl shadow-black/30 ring-1 ring-foreground/15 backdrop-blur-2xl supports-[backdrop-filter]:bg-card/65">
        <CardHeader className="gap-5 border-b border-foreground/10 bg-foreground/5 p-6 sm:p-8">
          <div className="flex items-center justify-between gap-4">
            <div className="flex size-12 items-center justify-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-black/20 ring-1 ring-primary/40">
              <ShieldCheckIcon aria-hidden="true" />
            </div>
            <Badge
              className="border-foreground/15 bg-foreground/5 text-muted-foreground"
              variant="outline"
            >
              <LockKeyholeIcon aria-hidden="true" />
              <span>Private console</span>
            </Badge>
          </div>
          <div className="flex flex-col gap-2">
            <CardTitle className="break-words text-2xl font-semibold tracking-tight sm:text-3xl">
              Login to {hostname}
            </CardTitle>
            <CardDescription className="max-w-sm text-sm leading-relaxed text-muted-foreground/90">
              Sign in with your Linux account to manage this host.
            </CardDescription>
          </div>
        </CardHeader>
        <form aria-busy={pending} className="flex flex-col" onSubmit={submit}>
          <CardContent className="p-6 sm:p-8">
            <FieldGroup>
              {error !== "" && (
                <Alert
                  className="border-destructive/40 bg-destructive/10 text-destructive"
                  variant="destructive"
                >
                  <AlertTitle>Sign-in failed</AlertTitle>
                  <AlertDescription className="text-destructive/90">
                    {error}
                  </AlertDescription>
                </Alert>
              )}
              {challenge ? (
                <LoginPromptFields prompts={challenge.prompts} />
              ) : (
                <>
                  <Field>
                    <FieldLabel
                      className="text-foreground/90"
                      htmlFor="username"
                    >
                      Username
                    </FieldLabel>
                    <InputGroup className={loginInputGroupClass}>
                      <InputGroupAddon className="pl-3.5 text-muted-foreground">
                        <UserIcon aria-hidden="true" />
                      </InputGroupAddon>
                      <InputGroupInput
                        id="username"
                        name="username"
                        autoComplete="username"
                        className="text-foreground placeholder:text-muted-foreground/70"
                        required
                        autoFocus
                      />
                    </InputGroup>
                  </Field>
                  <Field>
                    <FieldLabel
                      className="text-foreground/90"
                      htmlFor="password"
                    >
                      Password
                    </FieldLabel>
                    <InputGroup className={loginInputGroupClass}>
                      <InputGroupAddon className="pl-3.5 text-muted-foreground">
                        <LockKeyholeIcon aria-hidden="true" />
                      </InputGroupAddon>
                      <InputGroupInput
                        id="password"
                        name="password"
                        type="password"
                        autoComplete="current-password"
                        className="text-foreground placeholder:text-muted-foreground/70"
                      />
                    </InputGroup>
                    <FieldDescription className="text-muted-foreground/90">
                      Authentication follows this host&apos;s PAM policy.
                    </FieldDescription>
                  </Field>
                </>
              )}
            </FieldGroup>
          </CardContent>
          <CardFooter className="mt-0 flex-col gap-3 border-foreground/10 bg-foreground/5 p-6 sm:p-8">
            <Button
              className="h-11 w-full rounded-xl shadow-lg shadow-black/20"
              type="submit"
              disabled={pending}
            >
              {pending && (
                <LoaderCircleIcon
                  data-icon="inline-start"
                  className="animate-spin motion-reduce:animate-none"
                />
              )}
              {pending ? "Signing in…" : challenge ? "Continue" : "Sign in"}
            </Button>
            {challenge ? (
              <Button
                className="h-10 w-full text-muted-foreground hover:bg-foreground/5 hover:text-foreground"
                type="button"
                variant="ghost"
                onClick={cancelConversation}
                disabled={pending}
              >
                Start over
              </Button>
            ) : null}
            <p className="flex items-start gap-2 text-center text-xs leading-relaxed text-muted-foreground">
              <LockKeyholeIcon
                aria-hidden="true"
                className="mt-0.5 size-3.5 shrink-0"
              />
              <span>
                Credentials are passed to PAM and never stored by Tako.
              </span>
            </p>
          </CardFooter>
        </form>
      </Card>
    </LoginBackdrop>
  )
}
