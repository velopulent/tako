import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import * as React from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { api, type FileResult } from "@/lib/api"

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
  scope = "home",
  onDirtyChange,
  size = 0,
  previewToken,
  csrfToken,
}: {
  onDirtyChange?: (dirty: boolean) => void
  scope?: string
  size?: number
  path: string
  previewToken?: string
  csrfToken: string
}) {
  const queryClient = useQueryClient()
  const queryKey = ["file-content", scope, path, previewToken]
  const [draft, setDraft] = React.useState<string | undefined>(undefined)
  const content = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (size > 5 * 1024 * 1024)
        throw new Error("File is too large to edit. Download it instead.")
      const response = await fetch(
        `/api/v1/files/content?path=${encodeURIComponent(path)}&scope=${scope}`,
        { credentials: "same-origin", signal }
      )
      if (!response.ok) throw new Error("file read failed")
      return {
        text: await response.text(),
        fingerprint: response.headers.get("X-File-Fingerprint") ?? "",
      }
    },
  })
  const value = draft ?? content.data?.text ?? ""
  React.useEffect(() => {
    onDirtyChange?.(draft !== undefined && draft !== content.data?.text)
  }, [draft, content.data?.text, onDirtyChange])
  React.useEffect(() => () => onDirtyChange?.(false), [onDirtyChange])
  React.useEffect(() => {
    if (draft === undefined) return
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault()
    }
    window.addEventListener("beforeunload", warn)
    return () => window.removeEventListener("beforeunload", warn)
  }, [draft])
  const save = useMutation({
    mutationFn: (saved: string) =>
      api<FileResult>("/files", {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify({
          action: "write-text",
          scope,
          path,
          content: encodeContent(saved),
          expectedFingerprint: content.data?.fingerprint,
        }),
      }),
    onSuccess: (result, saved) => {
      queryClient.setQueryData(queryKey, {
        text: saved,
        fingerprint: result.entry?.fingerprint ?? "",
      })
      setDraft((current) => (current === saved ? undefined : current))
    },
  })
  if (content.isPending) return <Skeleton className="h-40" />
  if (content.isError)
    return (
      <Alert variant="destructive">
        <AlertTitle>Text unavailable</AlertTitle>
        <AlertDescription>{content.error.message}</AlertDescription>
      </Alert>
    )
  return (
    <div className="flex flex-col gap-2">
      <Textarea
        value={value}
        onChange={(event) => setDraft(event.target.value)}
        className="min-h-64 resize-y font-mono text-xs"
        spellCheck={false}
        aria-label={`Edit ${path}`}
      />
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">
          Changes save atomically. Maximum size: 5 MiB.
        </span>
        <Button
          onClick={() => save.mutate(value)}
          disabled={
            save.isPending ||
            content.data === undefined ||
            draft === undefined ||
            draft === content.data.text
          }
        >
          Save
        </Button>
      </div>
      {save.isError && (
        <Alert variant="destructive">
          <AlertTitle>Save rejected</AlertTitle>
          <AlertDescription>
            {save.error.message}
            <Button
              variant="outline"
              onClick={() => {
                void content.refetch()
              }}
            >
              Reload current file
            </Button>
          </AlertDescription>
        </Alert>
      )}
    </div>
  )
}
