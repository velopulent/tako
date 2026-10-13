import { screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { FileBrowser } from "@/components/file-browser"
import type { FileEntry } from "@/lib/api"
import { jsonResponse, renderWithProviders } from "@/test/render"

const entry = (overrides: Partial<FileEntry>): FileEntry => ({
  name: "notes.txt",
  path: "notes.txt",
  kind: "file",
  size: 12,
  mode: 0o644,
  modifiedAt: "2026-08-14T00:00:00Z",
  fingerprint: "entry",
  hidden: false,
  readable: true,
  writable: true,
  ...overrides,
})

function listing(entries: FileEntry[]) {
  return jsonResponse({
    directory: {
      path: ".",
      entries,
      showHidden: false,
      fingerprint: "directory",
    },
  })
}

afterEach(() => vi.unstubAllGlobals())

describe("FileBrowser", () => {
  it("shows one empty message for an empty folder", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => listing([]))
    )
    renderWithProviders(<FileBrowser />)
    expect(await screen.findByText(/This folder is empty/)).toBeTruthy()
  })

  it("shows a retryable error when the listing fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse(
          {
            code: "file-operation-failed",
            detail: "The file operation failed",
          },
          502
        )
      )
    )
    renderWithProviders(<FileBrowser />)
    expect(await screen.findByText("Could not load this data")).toBeTruthy()
    expect(screen.getByRole("button", { name: /Retry/ })).toBeTruthy()
  })

  it("keeps locked entries visible without offering actions", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        listing([
          entry({
            name: "private",
            path: "private",
            kind: "directory",
            mode: 0,
            readable: false,
            writable: false,
            permissionDenied: true,
          }),
        ])
      )
    )
    renderWithProviders(<FileBrowser />)
    expect(await screen.findByText("private")).toBeTruthy()
    expect(screen.getByLabelText("No permission")).toBeTruthy()
    expect(
      (
        screen.getByRole("button", {
          name: "Actions for private",
        }) as HTMLButtonElement
      ).disabled
    ).toBe(true)
  })

  it("lists folders before files and opens folders on click", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) =>
      String(input).includes("path=docs")
        ? listing([])
        : listing([
            entry({ name: "a.txt", path: "a.txt" }),
            entry({ name: "docs", path: "docs", kind: "directory" }),
          ])
    )
    vi.stubGlobal("fetch", fetchMock)
    renderWithProviders(<FileBrowser />)
    const rows = await screen.findAllByRole("row")
    expect(rows[1].textContent).toContain("docs")
    await userEvent.setup().click(screen.getByText("docs"))
    await vi.waitFor(() =>
      expect(String(fetchMock.mock.calls.at(-1)?.[0])).toContain("path=docs")
    )
  })

  it("keeps system files behind administrative access", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => listing([]))
    )
    renderWithProviders(<FileBrowser />)
    const system = await screen.findByLabelText("System")
    expect(
      system.hasAttribute("disabled") ||
        system.getAttribute("aria-disabled") === "true"
    ).toBe(true)
  })
})
