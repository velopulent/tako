import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ProcessDetails } from "@/components/process-details"

const process = {
  pid: 42,
  ppid: 1,
  started: 99,
  user: "operator",
  program: "worker",
  command: "/usr/bin/worker",
  state: "S",
  threads: 2,
  cpuTime: 1,
  memory: 1024,
  virtualMemory: 2048,
  diskRead: 10,
  diskWrite: 20,
}

function renderDetails() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ProcessDetails process={process} onClose={vi.fn()} />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("ProcessDetails", () => {
  it("shows relationships, history, files, and sockets", async () => {
    Element.prototype.getAnimations = () => [] as Animation[]
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              process,
              children: [],
              openFiles: ["/tmp/worker.sock"],
              sockets: [{ protocol: "tcp", local: "127.0.0.1:8080" }],
              history: [],
              cgroup: "0::/system.slice/worker.service",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } }
          )
        )
      )
    )
    renderDetails()
    expect(await screen.findByText("Relationships")).toBeTruthy()
    expect(screen.getByText("/tmp/worker.sock")).toBeTruthy()
    expect(screen.getByText("127.0.0.1:8080")).toBeTruthy()
  })

  it("surfaces a detail error", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response("", { status: 503 })))
    )
    renderDetails()
    expect(await screen.findByText("Process details unavailable")).toBeTruthy()
  })
})
