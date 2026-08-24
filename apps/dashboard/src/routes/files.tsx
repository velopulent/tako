import { createFileRoute } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"

import { api, type SessionResponse } from "@/lib/api"
import { FileBrowser } from "@/components/file-browser"
import { Page } from "@/lib/page"

function FilesPage() {
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  return (
    <Page description="Browse, preview, and manage files under authenticated UNIX authority.">
      <FileBrowser csrfToken={session.data?.csrfToken ?? ""} />
    </Page>
  )
}

export const Route = createFileRoute("/files")({
  component: FilesPage,
})
