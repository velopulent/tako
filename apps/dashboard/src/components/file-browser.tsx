import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import {
  ArrowUpIcon,
  ChevronRightIcon,
  EyeIcon,
  EyeOffIcon,
  FileArchiveIcon,
  FileCodeIcon,
  FileIcon,
  FileImageIcon,
  FileTextIcon,
  FileVideoIcon,
  FolderIcon,
  FolderPlusIcon,
  HouseIcon,
  LinkIcon,
  LockIcon,
  MoreHorizontalIcon,
  PauseIcon,
  PencilIcon,
  PlayIcon,
  RefreshCwIcon,
  SearchIcon,
  ServerIcon,
  Trash2Icon,
  UploadIcon,
  XIcon,
} from "lucide-react"
import * as React from "react"

import { useAdminAccess } from "@/components/admin-access"
import { DataTable, type DataTableFeatures } from "@/components/data-table"
import { MediaPreview } from "@/components/media-preview"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
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
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { InputGroup, InputGroupInput } from "@/components/ui/input-group"
import { Progress } from "@/components/ui/progress"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { toast } from "@/components/ui/toast"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { isNarrowViewport } from "@/hooks/use-mobile"
import { api, csrfHeaders, type FileEntry, type FileResult } from "@/lib/api"
import { formatBytes, formatDateTime, formatRelative } from "@/lib/format"
import { QueryError } from "@/lib/page"
import { cn } from "@/lib/utils"

type Scope = "home" | "system"
type ActionName =
  | "create"
  | "rename"
  | "move"
  | "copy"
  | "archive"
  | "extract"
  | "trash"
  | "restore"
  | "delete"
  | "metadata"
type Action = {
  action: ActionName
  entry?: FileEntry
  kind?: "file" | "directory"
}
type Transfer = {
  file: File
  path: string
  scope: Scope
  offset: number
  id: string
}

const chunkSize = 4 * 1024 * 1024
const actionLabels: Record<ActionName, string> = {
  create: "Create",
  rename: "Rename",
  move: "Move",
  copy: "Copy",
  archive: "Compress",
  extract: "Extract",
  trash: "Move to trash",
  restore: "Restore",
  delete: "Delete permanently",
  metadata: "Permissions",
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
const trashPath = (scope: Scope) =>
  scope === "system"
    ? "/root/.local/share/Trash/files"
    : ".local/share/Trash/files"
const archive = /\.(tar\.gz|tgz)$/i

function modeString(mode: number) {
  return [6, 3, 0]
    .map(
      (shift) =>
        `${mode & (4 << shift) ? "r" : "-"}${mode & (2 << shift) ? "w" : "-"}${mode & (1 << shift) ? "x" : "-"}`
    )
    .join("")
}

function EntryIcon({ entry }: { entry: FileEntry }) {
  const mime = entry.mime ?? ""
  const className = "size-4 shrink-0"
  if (entry.kind === "directory")
    return (
      <FolderIcon className={cn(className, "fill-primary/15 text-primary")} />
    )
  if (entry.kind === "symlink")
    return <LinkIcon className={cn(className, "text-muted-foreground")} />
  if (mime.startsWith("image/"))
    return <FileImageIcon className={cn(className, "text-muted-foreground")} />
  if (mime.startsWith("video/") || mime.startsWith("audio/"))
    return <FileVideoIcon className={cn(className, "text-muted-foreground")} />
  if (archive.test(entry.name) || mime.includes("zip") || mime.includes("tar"))
    return (
      <FileArchiveIcon className={cn(className, "text-muted-foreground")} />
    )
  if (
    mime.includes("json") ||
    mime.includes("javascript") ||
    /\.(sh|py|go|ts|js|rs|c|h|toml|ya?ml|conf|ini)$/i.test(entry.name)
  )
    return <FileCodeIcon className={cn(className, "text-muted-foreground")} />
  if (mime.startsWith("text/"))
    return <FileTextIcon className={cn(className, "text-muted-foreground")} />
  return <FileIcon className={cn(className, "text-muted-foreground")} />
}

export function FileBrowser() {
  const client = useQueryClient()
  const { administrative } = useAdminAccess()
  const [scope, setScope] = React.useState<Scope>("home")
  const [path, setPath] = React.useState(".")
  const [editingLocation, setEditingLocation] = React.useState(false)
  const [hidden, setHidden] = React.useState(false)
  const [pages, setPages] = React.useState<number[]>([0])
  const [fingerprint, setFingerprint] = React.useState("")
  const [filter, setFilter] = React.useState("")
  const [search, setSearch] = React.useState("")
  const [selected, setSelected] = React.useState<FileEntry | null>(null)
  const [checked, setChecked] = React.useState<Set<string>>(new Set())
  const [dirty, setDirty] = React.useState(false)
  const [discardPreview, setDiscardPreview] = React.useState(false)
  const [action, setAction] = React.useState<Action | null>(null)
  const [transfer, setTransfer] = React.useState<Transfer | null>(null)
  const [transferError, setTransferError] = React.useState("")
  const [uploading, setUploading] = React.useState(false)
  const [dragging, setDragging] = React.useState(false)
  const transferRef = React.useRef<Transfer | null>(null)
  const controller = React.useRef<AbortController | null>(null)
  const input = React.useRef<HTMLInputElement>(null)
  const offset = pages[pages.length - 1]
  const inTrash = path === trashPath(scope)

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
  const items =
    query.data?.search?.entries ?? query.data?.directory?.entries ?? []

  const refresh = () => {
    setPages([0])
    setFingerprint("")
    setChecked(new Set())
    void client.invalidateQueries({ queryKey: ["files"] })
  }
  const navigate = (next: string, nextScope: Scope = scope) => {
    setScope(nextScope)
    setSelected(null)
    setPath(next)
    setEditingLocation(false)
    setPages([0])
    setFingerprint("")
    setSearch("")
    setFilter("")
    setChecked(new Set())
  }
  // Losing administrative access while browsing system files returns home.
  const leaveSystem = React.useEffectEvent(() => navigate(".", "home"))
  React.useEffect(() => {
    if (scope === "system" && !administrative) leaveSystem()
  }, [scope, administrative])
  React.useEffect(() => () => controller.current?.abort(), [])

  const apply = (operation: Record<string, unknown>) =>
    api<FileResult>("/files", {
      method: "POST",
      body: JSON.stringify({ scope, ...operation }),
    })
  const mutation = useMutation({
    mutationFn: apply,
    onSuccess: (_, operation) => {
      toast.add({
        title: `${actionLabels[operation.action as ActionName] ?? "Change"} complete`,
        type: "success",
      })
      setAction(null)
      setSelected(null)
      refresh()
    },
  })
  const bulk = useMutation({
    mutationFn: async (operation: "trash" | "delete") => {
      for (const entry of items.filter((item) => checked.has(item.path))) {
        await apply({
          action: operation,
          path: entry.path,
          expectedFingerprint: entry.fingerprint,
          confirmation: "CONFIRM FILE OPERATION",
          permanent: operation === "delete",
          recursive: entry.kind === "directory",
        })
      }
    },
    onSuccess: refresh,
    onError: (error) => {
      toast.add({
        title: "Some items were not changed",
        description: error.message,
        type: "error",
      })
      refresh()
    },
  })

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

  const upload = async (task: Transfer) => {
    controller.current = new AbortController()
    setUploading(true)
    setTransferError("")
    transferRef.current = task
    setTransfer({ ...task })
    try {
      const recovery = await apply({
        action: "upload-status",
        path: task.path,
        scope: task.scope,
        ...(task.id ? { uploadId: task.id } : {}),
      })
      const candidates =
        recovery.uploads?.filter((item) => item.total === task.file.size) ?? []
      const resumable =
        candidates.find((item) => item.completed) ?? candidates[0]
      if (resumable?.completed) {
        finishUpload(task)
        return
      }
      if (resumable) {
        task.id = resumable.uploadId
        task.offset = resumable.offset
      } else if (task.id) {
        // The staging file expired. Restart at zero; the server still refuses
        // to replace an existing destination without its fingerprint.
        task.id = ""
        task.offset = 0
      }
      if (task.file.size === 0)
        await apply({
          action: "create",
          kind: "file",
          path: task.path,
          scope: task.scope,
        })
      while (task.offset < task.file.size) {
        const payload = await task.file
          .slice(task.offset, task.offset + chunkSize)
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
              ...csrfHeaders("POST"),
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
          throw new Error("The server returned an invalid upload offset")
        task.id = result.uploadId
        task.offset = result.offset
        setTransfer({ ...task })
      }
      finishUpload(task)
    } catch (cause) {
      setTransferError(
        cause instanceof Error && cause.name === "AbortError"
          ? "Paused"
          : cause instanceof Error
            ? cause.message
            : "Upload failed"
      )
    } finally {
      setUploading(false)
      if (input.current) input.current.value = ""
    }
  }
  const finishUpload = (task: Transfer) => {
    transferRef.current = null
    setTransfer(null)
    toast.add({ title: `Uploaded ${task.file.name}`, type: "success" })
    refresh()
  }
  const discard = async () => {
    const task = transferRef.current
    if (task?.id) {
      try {
        await apply({
          action: "cancel-upload",
          path: task.path,
          scope: task.scope,
          uploadId: task.id,
        })
      } catch (cause) {
        setTransferError(
          cause instanceof Error
            ? cause.message
            : "Could not discard the upload"
        )
        return
      }
    }
    transferRef.current = null
    setTransfer(null)
    setTransferError("")
  }
  const startUpload = (file?: File) => {
    if (!file || transfer) return
    void upload({
      file,
      path: joinPath(path, file.name),
      scope,
      offset: 0,
      id: "",
    })
  }

  const allChecked =
    items.length > 0 && items.every((item) => checked.has(item.path))
  const columns: ColumnDef<DataTableFeatures, FileEntry>[] = [
    {
      id: "select",
      size: 44,
      minSize: 44,
      enableSorting: false,
      header: () => (
        <Checkbox
          aria-label="Select all"
          checked={allChecked}
          indeterminate={checked.size > 0 && !allChecked}
          onCheckedChange={(on) =>
            setChecked(on ? new Set(items.map((item) => item.path)) : new Set())
          }
        />
      ),
      cell: ({ row }) => (
        // biome-ignore lint/a11y/noStaticElementInteractions: stops row activation when toggling selection
        // biome-ignore lint/a11y/useKeyWithClickEvents: the checkbox handles keyboard input
        <span onClick={(event) => event.stopPropagation()}>
          <Checkbox
            aria-label={`Select ${row.original.name}`}
            checked={checked.has(row.original.path)}
            onCheckedChange={(on) =>
              setChecked((current) => {
                const next = new Set(current)
                if (on) next.add(row.original.path)
                else next.delete(row.original.path)
                return next
              })
            }
          />
        </span>
      ),
    },
    {
      accessorKey: "name",
      header: "Name",
      size: 360,
      sortFn: (left: { original: FileEntry }, right: { original: FileEntry }) =>
        Number(right.original.kind === "directory") -
          Number(left.original.kind === "directory") ||
        left.original.name.localeCompare(right.original.name),
      cell: ({ row }) => (
        <span
          className={cn(
            "flex min-w-0 items-center gap-2.5",
            row.original.hidden && "opacity-70"
          )}
        >
          <EntryIcon entry={row.original} />
          <span className="truncate font-medium">
            {search ? row.original.path : row.original.name}
          </span>
          {row.original.symlinkTarget && (
            <span className="truncate text-xs text-muted-foreground">
              → {row.original.symlinkTarget}
            </span>
          )}
          {row.original.permissionDenied && (
            <LockIcon
              className="size-3.5 shrink-0 text-muted-foreground"
              aria-label="No permission"
            />
          )}
        </span>
      ),
    },
    {
      accessorKey: "size",
      header: "Size",
      size: 100,
      meta: { align: "end" },
      cell: ({ row }) => (
        <span className="text-muted-foreground tabular-nums">
          {row.original.kind === "directory"
            ? "—"
            : formatBytes(row.original.size)}
        </span>
      ),
    },
    {
      accessorKey: "modifiedAt",
      header: "Modified",
      size: 140,
      cell: ({ row }) => (
        <Tooltip>
          <TooltipTrigger render={<span className="text-muted-foreground" />}>
            {formatRelative(row.original.modifiedAt)}
          </TooltipTrigger>
          <TooltipContent>
            {formatDateTime(row.original.modifiedAt)}
          </TooltipContent>
        </Tooltip>
      ),
    },
    {
      accessorKey: "mode",
      header: "Permissions",
      size: 120,
      cell: ({ row }) => (
        <code className="text-xs text-muted-foreground">
          {modeString(row.original.mode)}
        </code>
      ),
    },
    {
      id: "actions",
      size: 52,
      minSize: 52,
      enableSorting: false,
      header: "",
      cell: ({ row }) => {
        const entry = row.original
        const options: ActionName[] = [
          "rename",
          "move",
          "copy",
          ...(entry.kind !== "symlink" ? (["archive"] as const) : []),
          ...(archive.test(entry.name) ? (["extract"] as const) : []),
          ...(scope === "system" ? (["metadata"] as const) : []),
        ]
        return (
          // biome-ignore lint/a11y/noStaticElementInteractions: stops row activation when opening the menu
          // biome-ignore lint/a11y/useKeyWithClickEvents: the trigger handles keyboard input
          <span onClick={(event) => event.stopPropagation()}>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Actions for ${entry.name}`}
                    disabled={entry.permissionDenied}
                  />
                }
              >
                <MoreHorizontalIcon />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuGroup>
                  <DropdownMenuItem onClick={() => open(entry)}>
                    Open
                  </DropdownMenuItem>
                  {entry.kind === "file" && (
                    <DropdownMenuItem onClick={() => download(entry)}>
                      Download
                    </DropdownMenuItem>
                  )}
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  {options.map((name) => (
                    <DropdownMenuItem
                      key={name}
                      onClick={() => setAction({ action: name, entry })}
                    >
                      {actionLabels[name]}…
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  {inTrash ? (
                    <>
                      <DropdownMenuItem
                        onClick={() => setAction({ action: "restore", entry })}
                      >
                        Restore
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => setAction({ action: "delete", entry })}
                      >
                        Delete permanently…
                      </DropdownMenuItem>
                    </>
                  ) : (
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={() => setAction({ action: "trash", entry })}
                    >
                      Move to trash
                    </DropdownMenuItem>
                  )}
                </DropdownMenuGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </span>
        )
      },
    },
  ]

  const crumbs = path.split("/").filter((part) => part && part !== ".")
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <ToggleGroup
          value={[scope]}
          onValueChange={(value) =>
            value[0] &&
            value[0] !== scope &&
            navigate(value[0] === "system" ? "/" : ".", value[0] as Scope)
          }
          variant="outline"
        >
          <ToggleGroupItem value="home" aria-label="My files">
            <HouseIcon />
            <span className="hidden sm:inline">My files</span>
          </ToggleGroupItem>
          <Tooltip>
            <TooltipTrigger render={<span />}>
              <ToggleGroupItem
                value="system"
                aria-label="System"
                disabled={!administrative}
              >
                <ServerIcon />
                <span className="hidden sm:inline">System</span>
              </ToggleGroupItem>
            </TooltipTrigger>
            {!administrative && (
              <TooltipContent>Requires administrative access</TooltipContent>
            )}
          </Tooltip>
        </ToggleGroup>
        <Button
          variant={inTrash ? "secondary" : "ghost"}
          onClick={() => navigate(trashPath(scope))}
        >
          <Trash2Icon data-icon="inline-start" />
          Trash
        </Button>
        <div className="ml-auto flex items-center gap-2">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="outline"
                  size="icon"
                  aria-label={
                    hidden ? "Hide hidden files" : "Show hidden files"
                  }
                  aria-pressed={hidden}
                  onClick={() => setHidden(!hidden)}
                />
              }
            >
              {hidden ? <EyeIcon /> : <EyeOffIcon />}
            </TooltipTrigger>
            <TooltipContent>
              {hidden ? "Hide hidden files" : "Show hidden files"}
            </TooltipContent>
          </Tooltip>
          <Button
            variant="outline"
            size="icon"
            aria-label="Refresh"
            onClick={refresh}
          >
            <RefreshCwIcon className={cn(query.isFetching && "animate-spin")} />
          </Button>
          <Button
            variant="outline"
            onClick={() => setAction({ action: "create", kind: "directory" })}
          >
            <FolderPlusIcon data-icon="inline-start" />
            <span className="hidden sm:inline">New folder</span>
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="outline"
                  size="icon"
                  aria-label="More create options"
                />
              }
            >
              <MoreHorizontalIcon />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuGroup>
                <DropdownMenuItem
                  onClick={() => setAction({ action: "create", kind: "file" })}
                >
                  New empty file
                </DropdownMenuItem>
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
          <input
            type="file"
            ref={input}
            className="hidden"
            onChange={(event) => startUpload(event.target.files?.[0])}
          />
          <Button
            onClick={() => input.current?.click()}
            disabled={transfer !== null}
          >
            <UploadIcon data-icon="inline-start" />
            Upload
          </Button>
        </div>
      </div>

      <div className="flex items-center gap-2">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Parent folder"
          disabled={path === "." || path === "/"}
          onClick={() =>
            navigate(query.data?.directory?.parent ?? parentPath(path))
          }
        >
          <ArrowUpIcon />
        </Button>
        {editingLocation ? (
          <form
            className="flex-1"
            onSubmit={(event) => {
              event.preventDefault()
              const value = new FormData(event.currentTarget).get("location")
              navigate(String(value ?? "").trim() || ".")
            }}
          >
            <InputGroup>
              <InputGroupInput
                name="location"
                aria-label="Location"
                className="font-mono text-xs"
                autoFocus
                defaultValue={path}
                onBlur={() => setEditingLocation(false)}
                onKeyDown={(event) =>
                  event.key === "Escape" && setEditingLocation(false)
                }
              />
            </InputGroup>
          </form>
        ) : (
          <nav
            aria-label="Location"
            className="flex min-w-0 flex-1 items-center overflow-x-auto rounded-md text-sm"
            onDoubleClick={() => setEditingLocation(true)}
          >
            <Button
              variant="ghost"
              size="sm"
              className="shrink-0"
              onClick={() => navigate(scope === "system" ? "/" : ".")}
            >
              {scope === "system" ? "/" : "Home"}
            </Button>
            {crumbs.map((part, index) => (
              <React.Fragment key={crumbs.slice(0, index + 1).join("/")}>
                <ChevronRightIcon className="size-3.5 shrink-0 text-muted-foreground" />
                <Button
                  variant="ghost"
                  size="sm"
                  className={cn(
                    "shrink-0",
                    index === crumbs.length - 1 && "font-semibold"
                  )}
                  onClick={() =>
                    navigate(
                      `${path.startsWith("/") ? "/" : ""}${crumbs.slice(0, index + 1).join("/")}`
                    )
                  }
                >
                  {part}
                </Button>
              </React.Fragment>
            ))}
            <Button
              variant="ghost"
              size="icon-sm"
              className="ml-1 shrink-0"
              aria-label="Type a location"
              onClick={() => setEditingLocation(true)}
            >
              <PencilIcon />
            </Button>
          </nav>
        )}
      </div>

      {transfer && (
        <div
          className="flex flex-col gap-2 rounded-lg border p-3"
          aria-live="polite"
        >
          <div className="flex items-center gap-2 text-sm">
            <UploadIcon className="size-4 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate">
              {transfer.file.name}
            </span>
            <span className="text-xs text-muted-foreground tabular-nums">
              {transferError ||
                `${formatBytes(transfer.offset)} of ${formatBytes(transfer.file.size)}`}
            </span>
            {uploading ? (
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Pause upload"
                onClick={() => controller.current?.abort()}
              >
                <PauseIcon />
              </Button>
            ) : (
              <>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Resume upload"
                  onClick={() =>
                    transferRef.current && void upload(transferRef.current)
                  }
                >
                  <PlayIcon />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Discard upload"
                  onClick={() => void discard()}
                >
                  <XIcon />
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

      {checked.size > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-sm">
          <span className="font-medium">{checked.size} selected</span>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setChecked(new Set())}
          >
            Clear
          </Button>
          <div className="ml-auto flex gap-2">
            {!inTrash && (
              <Button
                variant="outline"
                size="sm"
                disabled={bulk.isPending}
                onClick={() => bulk.mutate("trash")}
              >
                <Trash2Icon data-icon="inline-start" />
                Move to trash
              </Button>
            )}
            <Button
              variant="destructive"
              size="sm"
              disabled={bulk.isPending}
              onClick={() => bulk.mutate("delete")}
            >
              {bulk.isPending && <Spinner data-icon="inline-start" />}
              Delete permanently
            </Button>
          </div>
        </div>
      )}

      {query.isPending ? (
        <Skeleton className="h-96" />
      ) : query.isError ? (
        <QueryError error={query.error} retry={refresh} />
      ) : (
        // biome-ignore lint/a11y/noStaticElementInteractions: drop target for uploads; the Upload button is the accessible path
        <div
          className={cn(
            "relative rounded-lg",
            dragging &&
              "ring-2 ring-primary ring-offset-2 ring-offset-background"
          )}
          onDragOver={(event) => {
            if (!event.dataTransfer.types.includes("Files")) return
            event.preventDefault()
            setDragging(true)
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(event) => {
            event.preventDefault()
            setDragging(false)
            startUpload(event.dataTransfer.files[0])
          }}
        >
          <DataTable
            data={items}
            columns={columns}
            height="62vh"
            search={filter}
            onSearchChange={setFilter}
            searchPlaceholder={
              search ? `Results for “${search}”` : "Filter this folder"
            }
            emptyMessage={
              search
                ? "No matches in this folder or below."
                : inTrash
                  ? "Trash is empty."
                  : "This folder is empty. Drop a file here to upload it."
            }
            initialSorting={[{ id: "name", desc: false }]}
            initialVisibility={
              isNarrowViewport() ? { size: false, mode: false } : undefined
            }
            onRowClick={open}
            toolbar={
              search ? (
                <Button variant="outline" onClick={() => setSearch("")}>
                  <XIcon data-icon="inline-start" />
                  Clear search
                </Button>
              ) : (
                <Button
                  variant="outline"
                  disabled={!filter.trim()}
                  onClick={() => setSearch(filter.trim())}
                >
                  <SearchIcon data-icon="inline-start" />
                  <span className="hidden sm:inline">Search subfolders</span>
                </Button>
              )
            }
          />
        </div>
      )}

      {(query.data?.search?.limited ||
        pages.length > 1 ||
        query.data?.directory?.hasMore) && (
        <div className="flex items-center justify-between gap-2 text-sm text-muted-foreground">
          <span>
            {query.data?.search?.limited
              ? "Showing the first 500 matches. Narrow the folder to see more."
              : `${items.length} items on this page`}
          </span>
          {!search && (
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={pages.length === 1}
                onClick={() => setPages(pages.slice(0, -1))}
              >
                Previous
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={!query.data?.directory?.hasMore}
                onClick={() => {
                  setFingerprint(query.data?.directory?.fingerprint ?? "")
                  setPages([...pages, query.data?.directory?.nextOffset ?? 0])
                }}
              >
                Next
              </Button>
            </div>
          )}
        </div>
      )}

      <Sheet
        open={selected !== null}
        onOpenChange={(value) =>
          !value && (dirty ? setDiscardPreview(true) : setSelected(null))
        }
      >
        <SheetContent className="w-full gap-0 sm:max-w-3xl">
          <SheetHeader className="border-b">
            <SheetTitle className="truncate">{selected?.name}</SheetTitle>
            <SheetDescription className="truncate font-mono text-xs">
              {selected?.path} · {formatBytes(selected?.size ?? 0)} ·{" "}
              {selected && modeString(selected.mode)}
            </SheetDescription>
          </SheetHeader>
          {selected && (
            <MediaPreview
              key={`${scope}:${selected.path}`}
              entry={selected}
              scope={scope}
              onDirtyChange={setDirty}
            />
          )}
        </SheetContent>
      </Sheet>

      <Dialog open={discardPreview} onOpenChange={setDiscardPreview}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Discard unsaved changes?</DialogTitle>
            <DialogDescription>
              Your edits to {selected?.name} have not been saved.
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
              Discard
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {action && (
        <ActionDialog
          action={action}
          path={path}
          pending={mutation.isPending}
          error={mutation.error}
          onClose={() => {
            if (mutation.isPending) return
            mutation.reset()
            setAction(null)
          }}
          onSubmit={(operation) => mutation.mutate(operation)}
        />
      )}
    </div>
  )
}

function ActionDialog({
  action,
  path,
  pending,
  error,
  onClose,
  onSubmit,
}: {
  action: Action
  path: string
  pending: boolean
  error: Error | null
  onClose: () => void
  onSubmit: (operation: Record<string, unknown>) => void
}) {
  const entry = action.entry
  const initial =
    action.action === "metadata"
      ? (entry?.mode.toString(8) ?? "644")
      : action.action === "rename"
        ? (entry?.name ?? "")
        : action.action === "archive"
          ? joinPath(path, `${entry?.name ?? "archive"}.tar.gz`)
          : action.action === "extract"
            ? path
            : action.action === "move" || action.action === "copy"
              ? (entry?.path ?? "")
              : ""
  const [value, setValue] = React.useState(initial)
  const [owner, setOwner] = React.useState("")
  const [group, setGroup] = React.useState("")
  const [overwrite, setOverwrite] = React.useState(false)
  const needsValue = [
    "create",
    "rename",
    "move",
    "copy",
    "metadata",
    "archive",
    "extract",
  ].includes(action.action)
  const canOverwrite =
    ["rename", "move", "copy"].includes(action.action) && entry?.kind === "file"
  const valid =
    !needsValue ||
    (value.trim() !== "" &&
      (action.action !== "metadata" || /^[0-7]{3,4}$/.test(value)))
  const destructive = action.action === "delete" || overwrite
  const label =
    action.action === "metadata"
      ? "Mode (octal)"
      : action.action === "archive"
        ? "Archive path"
        : action.action === "extract" ||
            action.action === "move" ||
            action.action === "copy"
          ? "Destination"
          : "Name"
  const title =
    action.action === "create"
      ? action.kind === "directory"
        ? "New folder"
        : "New file"
      : `${actionLabels[action.action]} ${entry?.name ?? ""}`
  const description =
    action.action === "delete"
      ? "This cannot be undone."
      : action.action === "trash"
        ? "You can restore it from Trash."
        : action.action === "archive"
          ? "Creates a .tar.gz archive. Symbolic links are stored, not followed."
          : action.action === "extract"
            ? "Unpacks the archive into the destination folder."
            : action.action === "restore"
              ? "Moves the item back to where it was deleted from."
              : undefined
  const submit = () => {
    const operation: Record<string, unknown> = {
      action: action.action,
      path:
        action.action === "extract"
          ? value
          : (entry?.path ?? joinPath(path, value)),
      expectedFingerprint: entry?.fingerprint,
      confirmation: overwrite
        ? "CONFIRM FILE OVERWRITE"
        : "CONFIRM FILE OPERATION",
    }
    if (overwrite) operation.overwrite = true
    if (action.action === "create") operation.kind = action.kind
    if (action.action === "rename")
      operation.destination = joinPath(parentPath(entry?.path ?? path), value)
    if (action.action === "move" || action.action === "copy") {
      operation.destination = value
      if (entry?.kind === "directory") operation.recursive = true
    }
    if (action.action === "archive") operation.archivePath = value
    if (action.action === "extract") operation.archivePath = entry?.path
    if (action.action === "metadata") {
      operation.mode = Number.parseInt(value, 8)
      operation.owner = owner.trim() || undefined
      operation.group = group.trim() || undefined
    }
    if (action.action === "delete") {
      operation.permanent = true
      operation.recursive = entry?.kind === "directory"
    }
    onSubmit(operation)
  }
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <form
          className="flex flex-col gap-5"
          onSubmit={(event) => {
            event.preventDefault()
            submit()
          }}
        >
          <DialogHeader>
            <DialogTitle className="truncate">{title}</DialogTitle>
            {description && (
              <DialogDescription>{description}</DialogDescription>
            )}
          </DialogHeader>
          {needsValue && (
            <FieldGroup className="gap-3">
              <Field>
                <FieldLabel htmlFor="file-action-value">{label}</FieldLabel>
                <Input
                  id="file-action-value"
                  autoFocus
                  required
                  className={cn(
                    action.action !== "rename" &&
                      action.action !== "create" &&
                      "font-mono"
                  )}
                  value={value}
                  onChange={(event) => setValue(event.target.value)}
                />
              </Field>
              {action.action === "metadata" && (
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="file-owner">Owner</FieldLabel>
                    <Input
                      id="file-owner"
                      placeholder="Unchanged"
                      value={owner}
                      onChange={(event) => setOwner(event.target.value)}
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="file-group">Group</FieldLabel>
                    <Input
                      id="file-group"
                      placeholder="Unchanged"
                      value={group}
                      onChange={(event) => setGroup(event.target.value)}
                    />
                  </Field>
                </div>
              )}
              {canOverwrite && (
                <Field orientation="horizontal">
                  <Checkbox
                    id="file-overwrite"
                    checked={overwrite}
                    onCheckedChange={(on) => setOverwrite(on === true)}
                  />
                  <FieldLabel htmlFor="file-overwrite" className="font-normal">
                    Replace a file that already exists there
                  </FieldLabel>
                </Field>
              )}
            </FieldGroup>
          )}
          {error && (
            <Alert variant="destructive">
              <AlertTitle>Nothing changed</AlertTitle>
              <AlertDescription>{error.message}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={pending}
              onClick={onClose}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant={destructive ? "destructive" : "default"}
              disabled={!valid || pending}
            >
              {pending && <Spinner data-icon="inline-start" />}
              {action.action === "create"
                ? "Create"
                : actionLabels[action.action]}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
