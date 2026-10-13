import { DownloadIcon, FileQuestionIcon } from "lucide-react"

import { TextEditor } from "@/components/text-editor"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import type { FileEntry } from "@/lib/api"

const textTypes = [
  "application/json",
  "application/javascript",
  "application/xml",
  "application/x-sh",
  "application/toml",
  "application/yaml",
]

export function MediaPreview({
  entry,
  scope,
  onDirtyChange,
}: {
  entry: FileEntry
  scope: string
  onDirtyChange?: (dirty: boolean) => void
}) {
  const source = `/api/v1/files/content?${new URLSearchParams({ path: entry.path, scope, ...(entry.previewToken ? { token: entry.previewToken } : {}) })}`
  const mime = entry.mime ?? "application/octet-stream"
  const download = (
    <Button
      variant="outline"
      size="sm"
      render={<a href={source} download={entry.name} />}
    >
      <DownloadIcon data-icon="inline-start" />
      Download
    </Button>
  )
  if (mime.startsWith("text/") || textTypes.includes(mime)) {
    return (
      <TextEditor
        key={entry.path}
        scope={scope}
        path={entry.path}
        size={entry.size}
        writable={entry.writable}
        previewToken={entry.previewToken}
        onDirtyChange={onDirtyChange}
        download={download}
      />
    )
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-4">
      <div className="flex justify-end">{download}</div>
      <div className="flex flex-1 items-center justify-center rounded-lg bg-muted/40 p-2">
        {mime.startsWith("image/") ? (
          <img
            src={source}
            alt={entry.name}
            className="max-h-[75vh] max-w-full rounded object-contain"
          />
        ) : mime.startsWith("video/") ? (
          // biome-ignore lint/a11y/useMediaCaption: arbitrary host files have no caption tracks
          <video
            src={source}
            controls
            preload="metadata"
            className="max-h-[75vh] max-w-full rounded"
          />
        ) : mime.startsWith("audio/") ? (
          // biome-ignore lint/a11y/useMediaCaption: arbitrary host files have no caption tracks
          <audio src={source} controls preload="metadata" className="w-full" />
        ) : mime === "application/pdf" ? (
          <iframe
            src={source}
            title={entry.name}
            className="h-[75vh] w-full rounded bg-background"
          />
        ) : (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <FileQuestionIcon />
              </EmptyMedia>
              <EmptyTitle>No preview</EmptyTitle>
              <EmptyDescription>
                {mime} files cannot be shown in the browser.
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>{download}</EmptyContent>
          </Empty>
        )}
      </div>
    </div>
  )
}
