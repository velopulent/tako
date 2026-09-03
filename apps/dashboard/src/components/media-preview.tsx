import { DownloadIcon } from "lucide-react"
import { TextEditor } from "@/components/text-editor"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import type { FileEntry } from "@/lib/api"

export function MediaPreview({
  entry,
  csrfToken,
}: {
  entry: FileEntry | null
  csrfToken: string
}) {
  if (entry?.kind !== "file") return null
  const token = entry.previewToken
    ? `&token=${encodeURIComponent(entry.previewToken)}`
    : ""
  const source = `/api/v1/files/content?path=${encodeURIComponent(entry.path)}${token}`
  const mime = entry.mime ?? "application/octet-stream"
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="truncate text-base">
          Preview · {entry.name}
        </CardTitle>
        <Button
          variant="outline"
          size="sm"
          render={<a href={source} download={entry.name} />}
        >
          <DownloadIcon aria-hidden="true" /> Download
        </Button>
      </CardHeader>
      <CardContent>
        {mime.startsWith("image/") && (
          <img
            src={source}
            alt={entry.name}
            className="max-h-[60vh] max-w-full rounded object-contain"
          />
        )}
        {mime.startsWith("video/") && (
          // biome-ignore lint/a11y/useMediaCaption: previews arbitrary host files with no caption tracks available
          <video
            src={source}
            controls
            preload="metadata"
            className="max-h-[60vh] max-w-full"
          />
        )}
        {mime.startsWith("audio/") && (
          // biome-ignore lint/a11y/useMediaCaption: previews arbitrary host files with no caption tracks available
          <audio src={source} controls preload="metadata" className="w-full" />
        )}
        {mime === "application/pdf" && (
          <iframe
            src={source}
            title={entry.name}
            className="h-[60vh] w-full rounded border"
          />
        )}
        {(mime.startsWith("text/") ||
          mime === "application/json" ||
          mime === "application/javascript") && (
          <TextEditor
            path={entry.path}
            previewToken={entry.previewToken}
            csrfToken={csrfToken}
          />
        )}
        {!mime.startsWith("image/") &&
          !mime.startsWith("video/") &&
          !mime.startsWith("audio/") &&
          mime !== "application/pdf" &&
          !mime.startsWith("text/") &&
          mime !== "application/json" &&
          mime !== "application/javascript" && (
            <p className="text-sm text-muted-foreground">
              This file type has no safe browser preview. Download it or open it
              with a local application.
            </p>
          )}
      </CardContent>
    </Card>
  )
}
