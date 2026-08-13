import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ServiceConfiguration } from "@/components/service-configuration"

function renderConfiguration() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ServiceConfiguration scope="user" unit="demo.service" />
    </QueryClientProvider>
  )
}

afterEach(() => vi.unstubAllGlobals())

describe("ServiceConfiguration", () => {
  it("shows a loading placeholder before the bounded file response", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockReturnValue(new Promise<Response>(() => {}))
    )
    const { container } = renderConfiguration()
    expect(container.querySelector('[data-slot="skeleton"]')).not.toBeNull()
  })

  it("reports read failures without rendering arbitrary content", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({ title: "Unavailable", detail: "denied" }),
          {
            status: 503,
            headers: { "Content-Type": "application/problem+json" },
          }
        )
      )
    )
    renderConfiguration()
    expect(await screen.findByText("Configuration unavailable")).toBeTruthy()
    expect(screen.queryByText("[Service]")).toBeNull()
  })
})
