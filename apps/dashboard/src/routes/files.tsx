import { createFileRoute } from "@tanstack/react-router"

import { FileBrowser } from "@/components/file-browser"
import { Page } from "@/lib/page"

function FilesPage() {
  return (
    <Page>
      <FileBrowser />
    </Page>
  )
}

export const Route = createFileRoute("/files")({ component: FilesPage })
