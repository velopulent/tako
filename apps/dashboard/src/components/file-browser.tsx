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
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"

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
    if (entry.kind === "directory") setPath(entry.path)
    else
      window.open(
        `/api/v1/files/content?path=${encodePath(entry.path)}`,
        "_blank"
      )
  }
  const download = (entry: FileEntry) => {
    const anchor = document.createElement("a")
    anchor.href = `/api/v1/files/content?path=${encodePath(entry.path)}`
    anchor.download = entry.name
    anchor.click()
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
              <div
                key={`${entry.path}:${entry.fingerprint}`}
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
            ))}
          </div>
        )}
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
          <UploadIcon className="size-3.5" aria-hidden="true" /> Uploads and
          large transfers are available through the bounded file API;
          drag-and-drop is intentionally disabled until a destination is
          selected.
        </p>
      </CardContent>
    </Card>
  )
}
