import { createFileRoute } from "@tanstack/react-router"

import { UpdateInventory } from "@/components/update-inventory"
import { Page } from "@/lib/page"

function UpdatesPage() {
  return (
    <Page description="Installed-software update inventory without package-management writes.">
      <UpdateInventory />
    </Page>
  )
}

export const Route = createFileRoute("/updates")({
  component: UpdatesPage,
})
