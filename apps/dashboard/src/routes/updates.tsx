import { createFileRoute } from "@tanstack/react-router"

import { UpdateInventory } from "@/components/update-inventory"
import { Page } from "@/lib/page"

function UpdatesPage() {
  return (
    <Page description="Review pending packages, preview system changes, and apply updates safely.">
      <UpdateInventory />
    </Page>
  )
}

export const Route = createFileRoute("/updates")({
  component: UpdatesPage,
})
