import { createFileRoute } from "@tanstack/react-router"

import { SSHKeyManager } from "@/components/ssh-key-manager"

function SSHKeysTab() {
  return <SSHKeyManager />
}

export const Route = createFileRoute("/accounts/ssh-keys")({
  component: SSHKeysTab,
})
