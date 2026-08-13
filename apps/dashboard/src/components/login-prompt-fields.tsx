import { InfoIcon, LockKeyholeIcon } from "lucide-react"

import type { LoginPrompt } from "@/lib/api"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Field, FieldLabel } from "@/components/ui/field"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group"

export function LoginPromptFields({ prompts }: { prompts: LoginPrompt[] }) {
  return prompts.map((prompt) => {
    if (prompt.style === "info" || prompt.style === "error") {
      return (
        <Alert
          key={prompt.id}
          variant={prompt.style === "error" ? "destructive" : "default"}
        >
          <InfoIcon aria-hidden="true" />
          <AlertDescription>{prompt.message}</AlertDescription>
        </Alert>
      )
    }
    return (
      <Field key={prompt.id}>
        <FieldLabel htmlFor={`prompt-${prompt.id}`}>
          {prompt.message || "Authentication response"}
        </FieldLabel>
        <InputGroup>
          <InputGroupAddon>
            <LockKeyholeIcon aria-hidden="true" />
          </InputGroupAddon>
          <InputGroupInput
            id={`prompt-${prompt.id}`}
            name={prompt.id}
            type={prompt.style === "hidden" ? "password" : "text"}
            autoComplete={
              prompt.style === "hidden" ? "current-password" : "one-time-code"
            }
            required
            autoFocus
          />
        </InputGroup>
      </Field>
    )
  })
}
