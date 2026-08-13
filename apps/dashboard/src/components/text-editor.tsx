import * as React from "react"
import { useMutation, useQuery } from "@tanstack/react-query"

import { api, type FileResult } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"

function encodeContent(value: string) {
  const bytes = new TextEncoder().encode(value)
  let binary = ""
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte)
  })
  return btoa(binary)
}

export function TextEditor({
  path,
  csrfToken,
}: {
  path: string
  csrfToken: string
}) {
  const [draft, setDraft] = React.useState<string | undefined>(undefined)
  const content = useQuery({
    queryKey: ["file-content", path],
    queryFn: async () => {
      const response = await fetch(
        `/api/v1/files/content?path=${encodeURIComponent(path)}`,
        { credentials: "same-origin" }
      )
      if (!response.ok) throw new Error("file read failed")
      return {
        text: await response.text(),
        fingerprint: response.headers.get("X-File-Fingerprint") ?? "",
      }
    },
  })
  const value = draft ?? content.data?.text ?? ""
  const save = useMutation({
    mutationFn: () =>
      api<FileResult>("/files", {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify({
          action: "write-text",
          path,
          content: encodeContent(value),
          expectedFingerprint: content.data?.fingerprint,
        }),
      }),
    onSuccess: () => void content.refetch(),
  })
  if (content.isPending)
    return <div className="h-40 animate-pulse rounded bg-muted" />
  if (content.isError)
    return (
      <Alert variant="destructive">
        <AlertTitle>Text unavailable</AlertTitle>
        <AlertDescription>
          The file could not be read under the current UNIX authority.
        </AlertDescription>
      </Alert>
    )
  return (
    <div className="space-y-2">
      <Textarea
        value={value}
        onChange={(event) => setDraft(event.target.value)}
        className="min-h-64 resize-y font-mono text-xs"
        spellCheck={false}
        aria-label={`Edit ${path}`}
      />
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">
          Atomic save up to 5 MiB; stale fingerprints are rejected.
        </span>
        <Button
          onClick={() => save.mutate()}
          disabled={save.isPending || content.data === undefined}
        >
          Save
        </Button>
      </div>
      {save.isError && (
        <Alert variant="destructive">
          <AlertTitle>Save rejected</AlertTitle>
          <AlertDescription>
            The file changed or the user bridge lost write authority. Reload
            before retrying.
          </AlertDescription>
        </Alert>
      )}
    </div>
  )
}
