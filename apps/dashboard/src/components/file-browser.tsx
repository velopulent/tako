import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  CopyIcon,
  DownloadIcon,
  FileIcon,
  FolderIcon,
  MoreHorizontalIcon,
  RefreshCwIcon,
  Trash2Icon,
  UploadIcon,
} from "lucide-react"

import { api, type FileEntry, type FileResult } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { MediaPreview } from "@/components/media-preview"

const encodePath = (path: string) => encodeURIComponent(path)

function formatBytes(size: number) {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KiB`
  if (size < 1024 * 1024 * 1024)
    return `${(size / (1024 * 1024)).toFixed(1)} MiB`
  return `${(size / (1024 * 1024 * 1024)).toFixed(1)} GiB`
}

function parentPath(path: string) {
  const parts = path.split("/").filter(Boolean)
  parts.pop()
  return parts.length ? `/${parts.join("/")}` : "."
}

export function FileBrowser({ csrfToken }: { csrfToken: string }) {
  const queryClient = useQueryClient()
  const [path, setPath] = React.useState(".")
  const [showHidden, setShowHidden] = React.useState(false)
  const [query, setQuery] = React.useState("")
  const [selected, setSelected] = React.useState<FileEntry | null>(null)
  const [uploading, setUploading] = React.useState(false)
  const inputRef = React.useRef<HTMLInputElement>(null)
  const files = useQuery({
    queryKey: ["files", path, showHidden],
    queryFn: () =>
      api<FileResult>(
        `/files?path=${encodePath(path)}&hidden=${String(showHidden)}`
      ),
  })
  const mutation = useMutation({
    mutationFn: (operation: Record<string, unknown>) =>
      api<FileResult>("/files", {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify(operation),
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["files"] })
    },
  })

  const entries = (files.data?.directory?.entries ?? []).filter((entry) =>
    entry.name.toLowerCase().includes(query.toLowerCase())
  )
  const open = (entry: FileEntry) => {
    if (entry.permissionDenied) return
    if (entry.kind === "directory") {
      setSelected(null)
      setPath(entry.path)
    } else setSelected(entry)
  }
  const download = (entry: FileEntry) => {
    const anchor = document.createElement("a")
    anchor.href = `/api/v1/files/content?path=${encodePath(entry.path)}`
    anchor.download = entry.name
    anchor.click()
  }
  const upload = async (file: File) => {
    setUploading(true)
    try {
      const destination = path === "." ? file.name : `${path}/${file.name}`
      if (file.size === 0) {
        await api<FileResult>("/files", {
          method: "POST",
          headers: { "X-CSRF-Token": csrfToken },
          body: JSON.stringify({
            action: "create",
            path: destination,
            kind: "file",
          }),
        })
        void queryClient.invalidateQueries({ queryKey: ["files"] })
        return
      }
      let offset = 0
      const chunkSize = 4 * 1024 * 1024
      while (offset < file.size || (file.size === 0 && offset === 0)) {
        const chunk = file.slice(offset, offset + chunkSize)
        const payload = await chunk.arrayBuffer()
        const digest = await crypto.subtle.digest("SHA-256", payload)
        const checksum = Array.from(new Uint8Array(digest))
          .map((value) => value.toString(16).padStart(2, "0"))
          .join("")
        const response = await fetch(
          `/api/v1/files/upload?path=${encodePath(destination)}&offset=${offset}&total=${file.size}`,
          {
            method: "POST",
            credentials: "same-origin",
            headers: {
              "X-CSRF-Token": csrfToken,
              "X-Content-SHA256": checksum,
              "Content-Type": "application/octet-stream",
            },
            body: payload,
          }
        )
        if (!response.ok) throw new Error("upload failed")
        const result = (await response.json()) as { offset?: number }
        offset = result.offset ?? offset + payload.byteLength
        if (file.size === 0) break
      }
      void queryClient.invalidateQueries({ queryKey: ["files"] })
    } finally {
      setUploading(false)
      if (inputRef.current) inputRef.current.value = ""
    }
  }

  return (
    <Card>
      <CardHeader className="gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <CardTitle>Files</CardTitle>
          <CardDescription>
            Browse under your authenticated UNIX authority. Locked entries stay
            visible without leaking content.
          </CardDescription>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => void files.refetch()}
            aria-label="Refresh files"
          >
            <RefreshCwIcon aria-hidden="true" />
            Refresh
          </Button>
          <input
            ref={inputRef}
            type="file"
            className="hidden"
            onChange={(event) => {
              const file = event.target.files?.[0]
              if (file) void upload(file)
            }}
          />
          <Button
            variant="outline"
            size="sm"
            onClick={() => inputRef.current?.click()}
            disabled={uploading}
          >
            <UploadIcon aria-hidden="true" />{" "}
            {uploading ? "Uploading…" : "Upload"}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowHidden((value) => !value)}
            aria-pressed={showHidden}
          >
            {showHidden ? "Hide hidden" : "Show hidden"}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setPath(parentPath(path))}
            disabled={path === "."}
          >
            Up
          </Button>
          <code className="min-w-0 flex-1 truncate rounded bg-muted px-2 py-1 text-sm">
            {path}
          </code>
          <Input
            className="w-full sm:max-w-xs"
            placeholder="Filter this folder"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            aria-label="Filter files"
          />
        </div>
        {files.isPending && <Skeleton className="h-64" />}
        {files.isError && (
          <Alert variant="destructive">
            <AlertTitle>Files unavailable</AlertTitle>
            <AlertDescription>
              The authenticated UNIX bridge could not read this folder. Try
              refreshing or use the terminal.
            </AlertDescription>
          </Alert>
        )}
        {!files.isPending && !files.isError && entries.length === 0 && (
          <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
            This folder is empty or no entries match the filter.
          </div>
        )}
        {!files.isPending && !files.isError && entries.length > 0 && (
          <div
            className="divide-y rounded-lg border"
            role="table"
            aria-label="Files"
          >
            {entries.map((entry) => (
              <ContextMenu key={`${entry.path}:${entry.fingerprint}`}>
                <ContextMenuTrigger className="contents">
                  <div
                    className="flex min-h-14 items-center gap-3 px-3 py-2 hover:bg-muted/50"
                    role="row"
                    tabIndex={0}
                    onDoubleClick={() => open(entry)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault()
                        open(entry)
                      }
                    }}
                  >
                {entry.kind === "directory" ? (
                  <FolderIcon
                    className="size-5 text-muted-foreground"
                    aria-hidden="true"
                  />
                ) : (
                  <FileIcon
                    className="size-5 text-muted-foreground"
                    aria-hidden="true"
                  />
                )}
                <button
                  className="min-w-0 flex-1 truncate text-left font-medium"
                  onClick={() => open(entry)}
                >
                  {entry.name}
                </button>
                {entry.permissionDenied && (
                  <span className="text-xs text-muted-foreground">Locked</span>
                )}
                <span className="hidden text-xs text-muted-foreground sm:inline">
                  {formatBytes(entry.size)}
                </span>
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Actions for ${entry.name}`}
                      />
                    }
                  >
                    <MoreHorizontalIcon aria-hidden="true" />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem
                      onClick={() => open(entry)}
                      disabled={entry.permissionDenied}
                    >
                      <FileIcon aria-hidden="true" /> Open
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() => download(entry)}
                      disabled={
                        entry.permissionDenied || entry.kind === "directory"
                      }
                    >
                      <DownloadIcon aria-hidden="true" /> Download
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() =>
                        mutation.mutate({
                          action: "copy",
                          path: entry.path,
                          destination: `${entry.path}.copy`,
                          expectedFingerprint: entry.fingerprint,
                        })
                      }
                      disabled={
                        entry.permissionDenied || entry.kind === "directory"
                      }
                    >
                      <CopyIcon aria-hidden="true" /> Copy here
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      className="text-destructive"
                      onClick={() =>
                        mutation.mutate({
                          action: "trash",
                          path: entry.path,
                          expectedFingerprint: entry.fingerprint,
                        })
                      }
                      disabled={entry.permissionDenied}
                    >
                      <Trash2Icon aria-hidden="true" /> Move to trash
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
                  </div>
                </ContextMenuTrigger>
                <ContextMenuContent>
                  <ContextMenuItem
                    onClick={() => open(entry)}
                    disabled={entry.permissionDenied}
                  >
                    <FileIcon aria-hidden="true" /> Open
                  </ContextMenuItem>
                  <ContextMenuItem
                    onClick={() => download(entry)}
                    disabled={
                      entry.permissionDenied || entry.kind === "directory"
                    }
                  >
                    <DownloadIcon aria-hidden="true" /> Download
                  </ContextMenuItem>
                  <ContextMenuItem
                    onClick={() =>
                      mutation.mutate({
                        action: "copy",
                        path: entry.path,
                        destination: `${entry.path}.copy`,
                        expectedFingerprint: entry.fingerprint,
                      })
                    }
                    disabled={
                      entry.permissionDenied || entry.kind === "directory"
                    }
                  >
                    <CopyIcon aria-hidden="true" /> Copy here
                  </ContextMenuItem>
                  <ContextMenuItem
                    className="text-destructive"
                    onClick={() =>
                      mutation.mutate({
                        action: "trash",
                        path: entry.path,
                        expectedFingerprint: entry.fingerprint,
                      })
                    }
                    disabled={entry.permissionDenied}
                  >
                    <Trash2Icon aria-hidden="true" /> Move to trash
                  </ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>
            ))}
          </div>
        )}
        <MediaPreview entry={selected} csrfToken={csrfToken} />
        {mutation.isError && (
          <Alert variant="destructive">
            <AlertTitle>File action failed</AlertTitle>
            <AlertDescription>
              Refresh the folder and retry after checking the item is still
              present.
            </AlertDescription>
          </Alert>
        )}
        <p className="flex items-center gap-2 text-xs text-muted-foreground">
          <UploadIcon className="size-3.5" aria-hidden="true" /> Uploads use
          resumable 4 MiB chunks with per-chunk SHA-256 verification.
        </p>
      </CardContent>
    </Card>
  )
}
