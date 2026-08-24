import { createFileRoute } from "@tanstack/react-router"

import { JournalBrowser } from "@/components/journal-browser"
import { logsSearch } from "@/lib/search"
import { Page } from "@/lib/page"

function LogsPage() {
  return (
    <Page description="Searchable, virtualized live journal with bounded browser memory.">
      <JournalBrowser />
    </Page>
  )
}

export const Route = createFileRoute("/logs")({
  validateSearch: logsSearch,
  component: LogsPage,
})
