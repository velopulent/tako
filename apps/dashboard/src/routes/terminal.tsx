import { useQuery } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import { TerminalIcon } from "lucide-react"
import * as React from "react"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type TerminalStatus } from "@/lib/api"
import { Page } from "@/lib/page"

const SystemTerminal = React.lazy(() =>
  import("@/components/system-terminal").then((value) => ({
    default: value.SystemTerminal,
  }))
)

function TerminalPage() {
  const query = useQuery({
    queryKey: ["terminal"],
    queryFn: () => api<TerminalStatus>("/terminal"),
  })
  return (
    <Page description="Interactive shell under authenticated UNIX identity.">
      {query.data?.available ? (
        <React.Suspense fallback={<Skeleton className="h-[65vh]" />}>
          <SystemTerminal />
        </React.Suspense>
      ) : (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <TerminalIcon />
            </EmptyMedia>
            <EmptyTitle>User bridge unavailable</EmptyTitle>
            <EmptyDescription>{query.data?.message}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
    </Page>
  )
}

export const Route = createFileRoute("/terminal")({
  component: TerminalPage,
})
