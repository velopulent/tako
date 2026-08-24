import { createFileRoute } from "@tanstack/react-router"

import { SecurityControls } from "@/components/security-controls"
import { Page } from "@/lib/page"

function SecurityPage() {
  return (
    <Page description="Inspect and narrowly remediate SELinux and AppArmor policy state.">
      <SecurityControls />
    </Page>
  )
}

export const Route = createFileRoute("/security")({
  component: SecurityPage,
})
