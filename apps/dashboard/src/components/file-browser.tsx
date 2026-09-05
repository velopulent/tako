import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import {
  ArrowUpIcon,
  FileIcon,
  FolderIcon,
  MoreHorizontalIcon,
  PlusIcon,
  RefreshCwIcon,
  UploadIcon,
} from "lucide-react"
import * as React from "react"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { MediaPreview } from "@/components/media-preview"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type FileEntry, type FileResult } from "@/lib/api"
import { bytes } from "@/lib/page"

type Action = { action: string; entry?: FileEntry; kind?: string }
type Transfer = {
  file: File
  path: string
  scope: string
  offset: number
  id: string
}
const parentPath = (path: string) => {
  const absolute = path.startsWith("/")
  const parts = path.split("/").filter((part) => part && part !== ".")
  parts.pop()
  return parts.length
    ? `${absolute ? "/" : ""}${parts.join("/")}`
    : absolute
      ? "/"
      : "."
}
const joinPath = (path: string, name: string) =>
  path === "." ? name : `${path.replace(/\/$/, "")}/${name}`

export function FileBrowser({
  csrfToken,
  administrative = false,
}: {
  csrfToken: string
  administrative?: boolean
}) {
  const client = useQueryClient()
  const [scope, setScope] = React.useState("home")
  const [path, setPath] = React.useState(".")
  const [location, setLocation] = React.useState(".")
  const [hidden, setHidden] = React.useState(false)
  const [pages, setPages] = React.useState<number[]>([0])
  const [fingerprint, setFingerprint] = React.useState("")
  const [filter, setFilter] = React.useState("")
  const [search, setSearch] = React.useState("")
  const [selected, setSelected] = React.useState<FileEntry | null>(null)
  const [dirty, setDirty] = React.useState(false)
  const [discardPreview, setDiscardPreview] = React.useState(false)
  const [action, setAction] = React.useState<Action | null>(null)
  const [value, setValue] = React.useState("")
  const [error, setError] = React.useState("")
  const [transfer, setTransfer] = React.useState<Transfer | null>(null)
  const transferRef = React.useRef<Transfer | null>(null)
  const [uploading, setUploading] = React.useState(false)
  const controller = React.useRef<AbortController | null>(null)
  const input = React.useRef<HTMLInputElement>(null)
  const offset = pages[pages.length - 1]
  const query = useQuery({
    queryKey: ["files", scope, path, hidden, offset, search],
    queryFn: ({ signal }) =>
      api<FileResult>(
        search
          ? `/files/search?${new URLSearchParams({ scope, path, query: search, maxEntries: "500" })}`
          : `/files?${new URLSearchParams({ scope, path, hidden: String(hidden), offset: String(offset), limit: "200", ...(offset ? { fingerprint } : {}) })}`,
        { signal }
      ),
  })
  const refresh = () => {
    setPages([0])
    setFingerprint("")
    void client.invalidateQueries({ queryKey: ["files"] })
  }
  const navigate = (next: string) => {
    setSelected(null)
    setPath(next)
    setLocation(next)
    setPages([0])
    setFingerprint("")
    setSearch("")
    setFilter("")
    setError("")
  }
  const mutation = useMutation({
    mutationFn: (operation: Record<string, unknown>) =>
      api<FileResult>("/files", {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
        body: JSON.stringify({ ...operation, scope }),
      }),
    onSuccess: () => {
      setAction(null)
      setSelected(null)
      refresh()
    },
  })
  React.useEffect(() => () => controller.current?.abort(), [])
  React.useEffect(() => {
    if (scope === "system" && !administrative) {
      setScope("home")
      setPath(".")
      setLocation(".")
      setPages([0])
      setSelected(null)
    }
  }, [scope, administrative])
  const items =
    query.data?.search?.entries ?? query.data?.directory?.entries ?? []
  const download = (entry: FileEntry) => {
    const anchor = document.createElement("a")
    anchor.href = `/api/v1/files/content?${new URLSearchParams({ path: entry.path, scope, ...(entry.previewToken ? { token: entry.previewToken } : {}) })}`
    anchor.download = entry.name
    anchor.click()
  }
  const open = (entry: FileEntry) => {
    if (entry.permissionDenied) return
    if (entry.kind === "directory") navigate(entry.path)
    else setSelected(entry)
  }
  const choose = (next: Action) => {
    mutation.reset()
    setAction(next)
    setValue(
      next.action === "metadata"
        ? (next.entry?.mode.toString(8) ?? "644")
        : next.action === "rename"
          ? (next.entry?.name ?? "")
          : ""
    )
  }
  const runAction = () => {
    if (!action) return
    const operation: Record<string, unknown> = {
      action: action.action,
      path: action.entry?.path ?? joinPath(path, value),
      expectedFingerprint: action.entry?.fingerprint,
      confirmation: "CONFIRM FILE OPERATION",
    }
    if (action.action === "create") operation.kind = action.kind
    if (["rename", "move", "copy"].includes(action.action))
      operation.destination =
        action.action === "rename"
          ? joinPath(parentPath(action.entry?.path ?? path), value)
          : value
    if (action.action === "metadata") operation.mode = Number.parseInt(value, 8)
    if (action.action === "delete") {
      operation.permanent = true
      operation.recursive = action.entry?.kind === "directory"
    }
    mutation.mutate(operation)
  }
  const upload = async (task: Transfer) => {
    controller.current = new AbortController()
    setUploading(true)
    setError("")
    transferRef.current = task
    setTransfer({ ...task })
    try {
      if (task.file.size === 0) {
        await api<FileResult>("/files", {
          method: "POST",
          headers: { "X-CSRF-Token": csrfToken },
          body: JSON.stringify({
            action: "create",
            kind: "file",
            path: task.path,
            scope: task.scope,
          }),
        })
      }
      while (task.offset < task.file.size) {
        const payload = await task.file
          .slice(task.offset, task.offset + 4 * 1024 * 1024)
          .arrayBuffer()
        const hash = await crypto.subtle.digest("SHA-256", payload)
        const checksum = Array.from(new Uint8Array(hash), (byte) =>
          byte.toString(16).padStart(2, "0")
        ).join("")
        const response = await fetch(
          `/api/v1/files/upload?${new URLSearchParams({ path: task.path, scope: task.scope, offset: String(task.offset), total: String(task.file.size), uploadId: task.id })}`,
          {
            method: "POST",
            credentials: "same-origin",
            signal: controller.current.signal,
            headers: {
              "X-CSRF-Token": csrfToken,
              "X-Content-SHA256": checksum,
              "Content-Type": "application/octet-stream",
            },
            body: payload,
          }
        )
        const result = await response.json()
        if (!response.ok) throw new Error(result.detail ?? "Upload failed")
        if (
          !Number.isFinite(result.offset) ||
          result.offset <= task.offset ||
          result.offset > task.file.size
        )
          throw new Error("Server returned an invalid upload offset")
        task.id = result.uploadId
        task.offset = result.offset
        setTransfer({ ...task })
      }
      setTransfer(null)
      transferRef.current = null
      refresh()
    } catch (cause) {
      setError(
        cause instanceof Error && cause.name === "AbortError"
          ? "Upload paused. Retry to resume or discard it."
          : cause instanceof Error
            ? cause.message
            : "Upload failed"
      )
    } finally {
      setUploading(false)
      if (input.current) input.current.value = ""
    }
  }
  const discard = async () => {
    const task = transferRef.current
    if (task?.id) {
      try {
        await api("/files", {
          method: "POST",
          headers: { "X-CSRF-Token": csrfToken },
          body: JSON.stringify({
            action: "cancel-upload",
            path: task.path,
            scope: task.scope,
            uploadId: task.id,
          }),
        })
      } catch (cause) {
        setError(
          cause instanceof Error ? cause.message : "Could not discard upload"
        )
        return
      }
    }
    transferRef.current = null
    setTransfer(null)
    setError("")
  }
  const columns: ColumnDef<DataTableFeatures, FileEntry>[] = [
    {
      accessorKey: "name",
      header: "Name",
      size: 320,
      cell: ({ row }) => (
        <Button
          variant="ghost"
          onClick={() => open(row.original)}
          disabled={row.original.permissionDenied}
          className="max-w-full justify-start"
        >
          <span aria-hidden="true">
            {row.original.kind === "directory" ? <FolderIcon /> : <FileIcon />}
          </span>
          <span className="truncate">{row.original.name}</span>
        </Button>
      ),
    },
    {
      accessorKey: "size",
      header: "Size",
      size: 110,
      cell: ({ row }) =>
        row.original.kind === "directory" ? "—" : bytes(row.original.size),
    },
    {
      accessorKey: "modifiedAt",
      header: "Modified",
      size: 180,
      cell: ({ row }) => new Date(row.original.modifiedAt).toLocaleString(),
    },
    {
      accessorKey: "mode",
      header: "Permissions",
      size: 120,
      cell: ({ row }) =>
        row.original.permissionDenied ? (
          <Badge variant="outline">Locked</Badge>
        ) : (
          <code>{row.original.mode.toString(8).padStart(3, "0")}</code>
        ),
    },
    {
      id: "actions",
      header: "",
      size: 60,
      cell: ({ row }) => (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon"
                aria-label={`Actions for ${row.original.name}`}
              />
            }
          >
            <MoreHorizontalIcon />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuGroup>
              <DropdownMenuItem
                disabled={row.original.permissionDenied}
                onClick={() => open(row.original)}
              >
                Open
              </DropdownMenuItem>
              <DropdownMenuItem
                disabled={
                  row.original.permissionDenied || row.original.kind !== "file"
                }
                onClick={() => download(row.original)}
              >
                Download
              </DropdownMenuItem>
              {[
                "rename",
                "move",
                "copy",
                "trash",
                ...(path.includes(".local/share/Trash/files")
                  ? ["restore", "delete"]
                  : []),
                ...(scope === "system" ? ["metadata"] : []),
              ].map((name) => (
                <DropdownMenuItem
                  key={name}
                  disabled={
                    row.original.permissionDenied ||
                    (name === "copy" && row.original.kind !== "file")
                  }
                  onClick={() => choose({ action: name, entry: row.original })}
                >
                  {name === "metadata"
                    ? "Permissions"
                    : name === "trash"
                      ? "Move to trash"
                      : name[0].toUpperCase() + name.slice(1)}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      ),
    },
  ]
  const needsValue =
    action &&
    ["create", "rename", "move", "copy", "metadata"].includes(action.action)
  const validValue =
    !needsValue ||
    (value.trim() !== "" &&
      (action.action !== "metadata" || /^[0-7]{3,4}$/.test(value)))
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={scope === "system" ? "destructive" : "secondary"}>
          {scope === "system" ? "System · elevated" : "Home"}
        </Badge>
        <Button
          variant="outline"
          onClick={() => {
            setScope(scope === "home" ? "system" : "home")
            navigate(scope === "home" ? "/" : ".")
          }}
          disabled={!administrative && scope === "home"}
        >
          {scope === "home" ? "System files" : "My files"}
        </Button>
        <Button
          variant="ghost"
          onClick={() =>
            navigate(
              scope === "system"
                ? "/.local/share/Trash/files"
                : ".local/share/Trash/files"
            )
          }
        >
          Trash
        </Button>
        <div className="flex-1" />
        <Button
          variant="outline"
          onClick={() => setHidden(!hidden)}
          aria-pressed={hidden}
        >
          {hidden ? "Hide hidden" : "Show hidden"}
        </Button>
        <Button variant="outline" onClick={refresh} aria-label="Refresh files">
          <RefreshCwIcon />
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant="outline" />}>
            <PlusIcon data-icon="inline-start" />
            New
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuGroup>
              <DropdownMenuItem
                onClick={() => choose({ action: "create", kind: "directory" })}
              >
                Folder
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => choose({ action: "create", kind: "file" })}
              >
                File
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <input
          type="file"
          ref={input}
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0]
            if (file)
              void upload({
                file,
                path: joinPath(path, file.name),
                scope,
                offset: 0,
                id: "",
              })
          }}
        />
        <Button
          onClick={() => input.current?.click()}
          disabled={transfer !== null}
        >
          <UploadIcon data-icon="inline-start" />
          Upload
        </Button>
      </div>
      <form
        className="flex items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          navigate(location.trim() || ".")
        }}
      >
        <Button
          variant="outline"
          type="button"
          aria-label="Up"
          onClick={() =>
            navigate(query.data?.directory?.parent ?? parentPath(path))
          }
          disabled={path === "." || path === "/"}
        >
          <ArrowUpIcon />
        </Button>
        <Field className="flex-1">
          <FieldLabel htmlFor="file-location">Location</FieldLabel>
          <Input
            id="file-location"
            value={location}
            onChange={(event) => setLocation(event.target.value)}
          />
        </Field>
        <Button variant="outline" type="submit">
          Go
        </Button>
      </form>
      <nav aria-label="Folder breadcrumbs" className="flex flex-wrap gap-1">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate(scope === "system" ? "/" : ".")}
        >
          {scope === "system" ? "System" : "Home"}
        </Button>
        {path
          .split("/")
          .filter((part) => part && part !== ".")
          .map((part, index, parts) => (
            <Button
              key={parts.slice(0, index + 1).join("/")}
              variant="ghost"
              size="sm"
              onClick={() =>
                navigate(
                  `${scope === "system" ? "/" : ""}${parts.slice(0, index + 1).join("/")}`
                )
              }
            >
              {part}
            </Button>
          ))}
      </nav>
      {transfer && (
        <div className="flex flex-col gap-2" aria-live="polite">
          <div className="flex items-center gap-2">
            <span className="flex-1 truncate">
              {transfer.file.name} · {bytes(transfer.offset)} /{" "}
              {bytes(transfer.file.size)}
            </span>
            {uploading ? (
              <Button
                variant="outline"
                onClick={() => controller.current?.abort()}
              >
                Pause
              </Button>
            ) : (
              <>
                <Button
                  onClick={() =>
                    transferRef.current && void upload(transferRef.current)
                  }
                >
                  Retry
                </Button>
                <Button variant="outline" onClick={() => void discard()}>
                  Discard
                </Button>
              </>
            )}
          </div>
          <Progress
            value={
              transfer.file.size
                ? (transfer.offset / transfer.file.size) * 100
                : 0
            }
          />
        </div>
      )}
      {error && (
        <Alert variant="destructive">
          <AlertTitle>Transfer interrupted</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {query.isPending && <Skeleton className="h-80" />}
      {query.isError && (
        <Alert variant="destructive">
          <AlertTitle>Files unavailable</AlertTitle>
          <AlertDescription>
            {query.error.message}
            <Button variant="outline" onClick={refresh}>
              Retry
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {!query.isPending && !query.isError && items.length === 0 && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>This folder is empty</EmptyTitle>
            <EmptyDescription>
              {search
                ? "No matching names found. Clear search or choose another folder."
                : "Create a folder or upload a file to get started."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {!query.isPending && !query.isError && (
        <DataTable
          data={items}
          columns={columns}
          height="52vh"
          search={filter}
          onSearchChange={setFilter}
          searchPlaceholder="Filter names on this page"
          toolbar={
            <>
              <Button
                variant="outline"
                disabled={!filter.trim()}
                onClick={() => {
                  setSearch(filter)
                  setPages([0])
                }}
              >
                Search subfolders
              </Button>
              {search && (
                <Button variant="ghost" onClick={() => setSearch("")}>
                  Clear search
                </Button>
              )}
            </>
          }
        />
      )}
      {query.data?.search?.limited && (
        <p className="text-sm text-muted-foreground">
          Search limit reached. Choose a narrower folder.
        </p>
      )}
      <div className="flex items-center justify-between">
        <span className="text-sm text-muted-foreground">
          {items.length} items on this page
        </span>
        <div className="flex gap-2">
          <Button
            variant="outline"
            disabled={pages.length === 1}
            onClick={() => setPages(pages.slice(0, -1))}
          >
            Previous
          </Button>
          <Button
            variant="outline"
            disabled={!query.data?.directory?.hasMore}
            onClick={() => {
              setFingerprint(query.data?.directory?.fingerprint ?? "")
              setPages([...pages, query.data?.directory?.nextOffset ?? 0])
            }}
          >
            Next
          </Button>
        </div>
      </div>
      <Sheet
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) {
            if (dirty) setDiscardPreview(true)
            else setSelected(null)
          }
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-2xl">
          <SheetHeader>
            <SheetTitle>{selected?.name ?? "File details"}</SheetTitle>
            <SheetDescription>{selected?.path}</SheetDescription>
          </SheetHeader>
          <div className="p-4">
            <MediaPreview
              key={`${scope}:${selected?.path}`}
              entry={selected}
              csrfToken={csrfToken}
              scope={scope}
              onDirtyChange={setDirty}
            />
          </div>
        </SheetContent>
      </Sheet>
      <Dialog open={discardPreview} onOpenChange={setDiscardPreview}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Discard unsaved changes?</DialogTitle>
            <DialogDescription>
              Your edits have not been saved.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDiscardPreview(false)}>
              Keep editing
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                setDiscardPreview(false)
                setSelected(null)
                setDirty(false)
              }}
            >
              Discard changes
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={action !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setAction(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {action?.action === "create"
                ? `New ${action.kind}`
                : action?.action === "metadata"
                  ? "Change permissions"
                  : `${action?.action ?? "File action"} ${action?.entry?.name ?? ""}`}
            </DialogTitle>
            <DialogDescription>
              {action?.action === "delete"
                ? "This permanently deletes the item and cannot be undone."
                : action?.action === "trash"
                  ? "Move this item to trash. Restore it later from Trash."
                  : "Review the destination and apply this file change."}
            </DialogDescription>
          </DialogHeader>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault()
              runAction()
            }}
          >
            {needsValue && (
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="file-action-value">
                    {action.action === "metadata"
                      ? "Permissions (octal)"
                      : ["move", "copy"].includes(action.action)
                        ? "Destination path"
                        : "Name"}
                  </FieldLabel>
                  <Input
                    id="file-action-value"
                    value={value}
                    onChange={(event) => setValue(event.target.value)}
                    required
                  />
                </Field>
              </FieldGroup>
            )}
            {mutation.isError && (
              <Alert variant="destructive">
                <AlertTitle>File action failed</AlertTitle>
                <AlertDescription>{mutation.error.message}</AlertDescription>
              </Alert>
            )}
            <DialogFooter>
              <Button
                variant="outline"
                type="button"
                disabled={mutation.isPending}
                onClick={() => setAction(null)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={!validValue || mutation.isPending}
              >
                {mutation.isPending ? "Applying…" : "Apply"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
