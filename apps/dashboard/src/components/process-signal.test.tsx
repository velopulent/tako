import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ProcessSignal } from "@/components/process-signal"

const process = {
  pid: 42,
  ppid: 1,
  started: 99,
  uid: 1000,
  user: "operator",
  program: "worker",
  command: "/usr/bin/worker",
  state: "S",
  threads: 1,
  cpuTime: 1,
  memory: 1,
  virtualMemory: 1,
  diskRead: 1,
  diskWrite: 1,
}

afterEach(() => vi.unstubAllGlobals())

describe("ProcessSignal", () => {
  it("requires an impact preview before applying", async () => {
    Element.prototype.getAnimations = () => [] as Animation[]
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url.endsWith("/auth/session"))
          return new Response(JSON.stringify({ csrfToken: "csrf" }))
        if (url.endsWith("/signal/preview")) {
          return new Response(
            JSON.stringify({
              signal: "TERM",
              tree: false,
              fingerprint: "f".repeat(64),
              targets: [
                {
                  pid: 42,
                  started: 99,
                  uid: 1000,
                  user: "operator",
                  program: "worker",
                },
              ],
            })
          )
        }
        if (url.endsWith("/signal") && init?.method === "POST") {
          return new Response(
            JSON.stringify({
              signal: "TERM",
              tree: false,
              targets: [
                {
                  pid: 42,
                  started: 99,
                  uid: 1000,
                  user: "operator",
                  program: "worker",
                },
              ],
              signaled: [
                {
                  pid: 42,
                  started: 99,
                  uid: 1000,
                  user: "operator",
                  program: "worker",
                },
              ],
            })
          )
        }
        return new Response("not found", { status: 404 })
      }
    )
    vi.stubGlobal("fetch", fetchMock)
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={client}>
        <ProcessSignal process={process} />
      </QueryClientProvider>
    )
    expect(await screen.findByRole("button", { name: /Send TERM/ })).toBeTruthy()
    await user.click(screen.getByRole("button", { name: /Send TERM/ }))
    expect(await screen.findByText(/will affect 1 process/)).toBeTruthy()
    await user.click(screen.getByRole("button", { name: /Confirm and send TERM/ }))
    await vi.waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([, init]) =>
            init?.method === "POST" &&
            String(init.body).includes("expectedTargets")
        )
      ).toBe(true)
    )
  })
})
