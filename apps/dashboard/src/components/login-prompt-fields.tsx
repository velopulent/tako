import { InfoIcon, LockKeyholeIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Field, FieldLabel } from "@/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group"
import type { LoginPrompt } from "@/lib/api"
import { cn } from "@/lib/utils"

export function LoginPromptFields({ prompts }: { prompts: LoginPrompt[] }) {
  return prompts.map((prompt) => {
    if (prompt.style === "info" || prompt.style === "error") {
      return (
        <Alert
          key={prompt.id}
          className={cn(
            "border-foreground/10 bg-foreground/5 text-foreground",
            prompt.style === "error" &&
              "border-destructive/40 bg-destructive/10 text-destructive"
          )}
          variant={prompt.style === "error" ? "destructive" : "default"}
        >
          <InfoIcon aria-hidden="true" />
          <AlertDescription
            className={cn(
              prompt.style === "error"
                ? "text-destructive/90"
                : "text-muted-foreground"
            )}
          >
            {prompt.message}
          </AlertDescription>
        </Alert>
      )
    }
    return (
      <Field key={prompt.id}>
        <FieldLabel
          className="text-foreground/90"
          htmlFor={`prompt-${prompt.id}`}
        >
          {prompt.message || "Authentication response"}
        </FieldLabel>
        <InputGroup className="h-12 rounded-xl border-foreground/15 bg-foreground/5 shadow-sm transition-colors focus-within:border-primary/60 focus-within:bg-foreground/10">
          <InputGroupAddon className="pl-3.5 text-muted-foreground">
            <LockKeyholeIcon aria-hidden="true" />
          </InputGroupAddon>
          <InputGroupInput
            id={`prompt-${prompt.id}`}
            name={prompt.id}
            type={prompt.style === "hidden" ? "password" : "text"}
            autoComplete={
              prompt.style === "hidden" ? "current-password" : "one-time-code"
            }
            className="text-foreground placeholder:text-muted-foreground/70"
            required
            autoFocus
          />
        </InputGroup>
      </Field>
    )
  })
}
