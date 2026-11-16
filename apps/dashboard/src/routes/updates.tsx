import { createFileRoute } from "@tanstack/react-router"

import { UpdateInventory } from "@/components/update-inventory"
import { Page } from "@/lib/page"

function UpdatesPage() {
  return (
    <Page description="See what available updates fix, review the changes, and install when you’re ready.">
      <UpdateInventory />
    </Page>
  )
}

export const Route = createFileRoute("/updates")({
  component: UpdatesPage,
})
