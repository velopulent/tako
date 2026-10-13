import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import * as React from "react"

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Kbd } from "@/components/ui/kbd"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { api, type FileResult } from "@/lib/api"

const maxEditable = 5 * 1024 * 1024

function encodeContent(value: string) {
  const bytes = new TextEncoder().encode(value)
  let binary = ""
  for (let index = 0; index < bytes.length; index += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(index, index + 0x8000))
  }
  return btoa(binary)
}

export function TextEditor({
  path,
  scope,
  size,
  writable,
  previewToken,
  onDirtyChange,
  download,
}: {
  path: string
  scope: string
  size: number
  writable: boolean
  previewToken?: string
  onDirtyChange?: (dirty: boolean) => void
  download: React.ReactNode
}) {
  const client = useQueryClient()
  const queryKey = ["file-content", scope, path, previewToken]
  const [draft, setDraft] = React.useState<string>()
  const content = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (size > maxEditable)
        throw new Error("This file is larger than 5 MiB. Download it instead.")
      const response = await fetch(
        `/api/v1/files/content?${new URLSearchParams({ path, scope, ...(previewToken ? { token: previewToken } : {}) })}`,
        { credentials: "same-origin", signal }
      )
      if (!response.ok) throw new Error("The file could not be read.")
      return {
        text: await response.text(),
        fingerprint: response.headers.get("X-File-Fingerprint") ?? "",
      }
    },
    staleTime: Number.POSITIVE_INFINITY,
  })
  const dirty = draft !== undefined && draft !== content.data?.text
  React.useEffect(() => onDirtyChange?.(dirty), [dirty, onDirtyChange])
  React.useEffect(() => () => onDirtyChange?.(false), [onDirtyChange])
  React.useEffect(() => {
    if (!dirty) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener("beforeunload", warn)
    return () => window.removeEventListener("beforeunload", warn)
  }, [dirty])
  const save = useMutation({
    mutationFn: (saved: string) =>
      api<FileResult>("/files", {
        method: "POST",
        body: JSON.stringify({
          action: "write-text",
          scope,
          path,
          content: encodeContent(saved),
          expectedFingerprint: content.data?.fingerprint,
        }),
      }),
    onSuccess: (result, saved) => {
      client.setQueryData(queryKey, {
        text: saved,
        fingerprint: result.entry?.fingerprint ?? "",
      })
      setDraft((current) => (current === saved ? undefined : current))
      toast.add({ title: "Saved", type: "success" })
    },
  })
  const value = draft ?? content.data?.text ?? ""
  const canSave = writable && dirty && !save.isPending
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-2 border-b px-4 py-2">
        <span className="flex-1 text-xs text-muted-foreground">
          {!writable ? "Read-only" : dirty ? "Unsaved changes" : "Saved"}
        </span>
        {download}
        {writable && (
          <Button
            size="sm"
            disabled={!canSave}
            onClick={() => save.mutate(value)}
          >
            {save.isPending && <Spinner data-icon="inline-start" />}
            Save
            <Kbd className="ml-1 hidden sm:inline-flex">Ctrl S</Kbd>
          </Button>
        )}
      </div>
      {save.isError && (
        <Alert variant="destructive" className="m-4 mb-0 w-auto">
          <AlertTitle>Not saved</AlertTitle>
          <AlertDescription>{save.error.message}</AlertDescription>
          <AlertAction>
            <Button
              size="sm"
              variant="outline"
              onClick={() => void content.refetch()}
            >
              Reload file
            </Button>
          </AlertAction>
        </Alert>
      )}
      {content.isPending ? (
        <Skeleton className="m-4 flex-1" />
      ) : content.isError ? (
        <Alert className="m-4 w-auto">
          <AlertTitle>Cannot edit this file</AlertTitle>
          <AlertDescription>{content.error.message}</AlertDescription>
        </Alert>
      ) : (
        <textarea
          value={value}
          readOnly={!writable}
          spellCheck={false}
          wrap="off"
          aria-label={`Contents of ${path}`}
          className="min-h-0 flex-1 resize-none bg-transparent p-4 font-mono text-xs leading-5 outline-none"
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (
              (event.ctrlKey || event.metaKey) &&
              event.key.toLowerCase() === "s"
            ) {
              event.preventDefault()
              if (canSave) save.mutate(value)
            }
            if (event.key === "Tab" && !event.shiftKey && writable) {
              event.preventDefault()
              const target = event.currentTarget
              const { selectionStart, selectionEnd } = target
              setDraft(
                `${value.slice(0, selectionStart)}\t${value.slice(selectionEnd)}`
              )
              requestAnimationFrame(() =>
                target.setSelectionRange(selectionStart + 1, selectionStart + 1)
              )
            }
          }}
        />
      )}
    </div>
  )
}
