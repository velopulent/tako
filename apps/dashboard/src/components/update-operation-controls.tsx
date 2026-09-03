import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import type { UpdateOperation, UpdatePreview, UpdateStatus } from "@/lib/api"

export function UpdateOperationControls({
  status,
  scope,
  selected,
  preview,
  confirmation,
  previewing,
  applying,
  sessionReady,
  onScopeChange,
  onPreview,
  onConfirmationChange,
  onApply,
}: {
  status: UpdateStatus
  scope: UpdateOperation["scope"]
  selected: string[]
  preview?: UpdatePreview
  confirmation: string
  previewing: boolean
  applying: boolean
  sessionReady: boolean
  onScopeChange: (scope: UpdateOperation["scope"]) => void
  onPreview: () => void
  onConfirmationChange: (value: string) => void
  onApply: () => void
}) {
  const canPreview =
    status.available &&
    !status.externalLock &&
    (scope === "all" || selected.length > 0)
  return (
    <>
      <FieldGroup className="rounded-md border p-3 sm:flex-row sm:items-end">
        <Field>
          <FieldLabel>Update scope</FieldLabel>
          {/* biome-ignore lint/a11y/useSemanticElements: labeled scope-switcher button pair; a fieldset restyle is out of scope */}
          <div className="flex gap-2" role="group" aria-label="Update scope">
            <Button
              type="button"
              size="sm"
              variant={scope === "all" ? "default" : "outline"}
              onClick={() => onScopeChange("all")}
            >
              All packages
            </Button>
            <Button
              type="button"
              size="sm"
              variant={scope === "selected" ? "default" : "outline"}
              onClick={() => onScopeChange("selected")}
            >
              Selected packages
            </Button>
          </div>
          <FieldDescription>
            Preview the current inventory before starting a serialized update
            job.
          </FieldDescription>
        </Field>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            disabled={!canPreview || previewing}
            onClick={onPreview}
          >
            {previewing ? "Previewing…" : "Preview updates"}
          </Button>
          {preview?.allowed && preview.requiresConfirmation && (
            <div className="flex items-end gap-2">
              <Input
                aria-label="Update confirmation"
                placeholder="Type APPLY UPDATES"
                value={confirmation}
                onChange={(event) => onConfirmationChange(event.target.value)}
              />
              <Button
                type="button"
                disabled={
                  confirmation !== "APPLY UPDATES" || !sessionReady || applying
                }
                onClick={onApply}
              >
                {applying ? "Starting…" : "Apply updates"}
              </Button>
            </div>
          )}
        </div>
      </FieldGroup>
      {preview && (
        <Alert variant={preview.allowed ? "default" : "destructive"}>
          <AlertTitle>
            {preview.allowed ? "Update preview" : "Update cannot start"}
          </AlertTitle>
          <AlertDescription>
            {preview.reason || preview.changes.join(", ")}
            {preview.warnings.length > 0 && (
              <span className="mt-1 block">{preview.warnings.join(" ")}</span>
            )}
          </AlertDescription>
        </Alert>
      )}
    </>
  )
}
