import { useQuery } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import { SquareTerminalIcon } from "lucide-react"
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
import { QueryError } from "@/lib/page"

const SystemTerminal = React.lazy(() =>
  import("@/components/system-terminal").then((module) => ({
    default: module.SystemTerminal,
  }))
)

function TerminalPage() {
  const query = useQuery({
    queryKey: ["terminal"],
    queryFn: () => api<TerminalStatus>("/terminal"),
  })
  return (
    <main className="flex h-[calc(100svh-var(--header-height))] min-h-0 flex-col p-4 md:h-[calc(100svh-var(--header-height)-1rem)] lg:p-6">
      {query.isPending ? (
        <Skeleton className="flex-1" />
      ) : query.isError ? (
        <QueryError error={query.error} retry={() => void query.refetch()} />
      ) : query.data.available ? (
        <React.Suspense fallback={<Skeleton className="flex-1" />}>
          <SystemTerminal />
        </React.Suspense>
      ) : (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SquareTerminalIcon />
            </EmptyMedia>
            <EmptyTitle>Terminal unavailable</EmptyTitle>
            <EmptyDescription>{query.data.message}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
    </main>
  )
}

export const Route = createFileRoute("/terminal")({ component: TerminalPage })
