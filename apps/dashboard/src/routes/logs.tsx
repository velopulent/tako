import { createFileRoute } from "@tanstack/react-router"

import { JournalBrowser } from "@/components/journal-browser"
import { Page } from "@/lib/page"
import { logsSearch } from "@/lib/search"

function LogsPage() {
  return (
    <Page>
      <JournalBrowser />
    </Page>
  )
}

export const Route = createFileRoute("/logs")({
  validateSearch: logsSearch,
  component: LogsPage,
})
