import { createFileRoute } from "@tanstack/react-router"

import { PasswordManager } from "@/components/password-manager"

function PasswordsTab() {
  return <PasswordManager />
}

export const Route = createFileRoute("/accounts/passwords")({
  component: PasswordsTab,
})
