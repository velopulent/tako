import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { FileBrowser } from "@/components/file-browser"

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }))

vi.mock("@/lib/api", () => ({ api: apiMock }))

function renderBrowser() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <FileBrowser csrfToken="csrf" />
    </QueryClientProvider>
  )
}

describe("FileBrowser", () => {
  it("shows an empty state after a successful bounded listing", async () => {
    apiMock.mockResolvedValueOnce({
      directory: { path: ".", entries: [], showHidden: false, fingerprint: "" },
    })
    renderBrowser()
    expect(await screen.findByText(/folder is empty/i)).toBeTruthy()
  })

  it("shows a recoverable error when the UNIX bridge is unavailable", async () => {
    apiMock.mockRejectedValueOnce(new Error("bridge unavailable"))
    renderBrowser()
    expect(await screen.findByText("Files unavailable")).toBeTruthy()
  })

  it("keeps locked entries visible without offering unsafe actions", async () => {
    apiMock.mockResolvedValueOnce({
      directory: {
        path: ".",
        showHidden: false,
        fingerprint: "directory",
        entries: [
          {
            name: "private",
            path: "private",
            kind: "directory",
            size: 0,
            mode: 0,
            modifiedAt: "2026-08-14T00:00:00Z",
            fingerprint: "entry",
            hidden: false,
            readable: false,
            writable: false,
            permissionDenied: true,
          },
        ],
      },
    })
    renderBrowser()
    expect(await screen.findByText("private")).toBeTruthy()
    expect(screen.getByText("Locked")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Actions for private" })).toBeTruthy()
  })
})
