import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SavedLogViews } from "@/components/saved-log-views"

function renderViews(onApply = vi.fn()) {
  return {
    onApply,
    ...render(
      <SavedLogViews filter={{ unit: "worker.service" }} onApply={onApply} />
    ),
  }
}

afterEach(() => {
  localStorage.clear()
  vi.unstubAllGlobals()
})

describe("SavedLogViews", () => {
  it("applies, creates, edits, and deletes a local view", async () => {
    localStorage.setItem(
      "tako-log-views:v1",
      JSON.stringify([
        {
          id: "view-1",
          name: "Incident",
          filter: { unit: "worker.service", priority: "3" },
          createdAt: "2026-08-13T00:00:00Z",
          updatedAt: "2026-08-13T00:00:00Z",
        },
      ])
    )
    const user = userEvent.setup()
    const { onApply } = renderViews()
    await user.click(await screen.findByRole("button", { name: "Apply" }))
    expect(onApply).toHaveBeenCalledWith({
      unit: "worker.service",
      priority: "3",
    })

    await user.type(screen.getByLabelText("Save current filters in this browser"), "New")
    await user.click(screen.getByRole("button", { name: "Save view" }))
    expect(await screen.findByText("New")).toBeTruthy()

    const renames = await screen.findAllByRole("button", { name: "Rename" })
    await user.click(renames[0])
    const rename = screen.getByRole("textbox", { name: "Rename Incident" })
    await user.clear(rename)
    await user.type(rename, "Incident renamed")
    await user.click(screen.getByRole("button", { name: "Save name" }))
    expect(await screen.findByText("Incident renamed")).toBeTruthy()

    const deletes = await screen.findAllByRole("button", { name: "Delete" })
    await user.click(deletes[0])
    await vi.waitFor(() => {
      expect(screen.queryByText("Incident renamed")).toBeNull()
    })
    const stored = JSON.parse(
      localStorage.getItem("tako-log-views:v1") ?? "[]"
    ) as { name: string }[]
    expect(stored.some((view) => view.name === "New")).toBe(true)
  })

  it("rejects duplicate names", async () => {
    localStorage.setItem(
      "tako-log-views:v1",
      JSON.stringify([
        {
          id: "view-1",
          name: "Incident",
          filter: { unit: "worker.service" },
          createdAt: "2026-08-13T00:00:00Z",
          updatedAt: "2026-08-13T00:00:00Z",
        },
      ])
    )
    const user = userEvent.setup()
    renderViews()
    await screen.findByText("Incident")
    await user.type(
      screen.getByLabelText("Save current filters in this browser"),
      "incident"
    )
    await user.click(screen.getByRole("button", { name: "Save view" }))
    expect(
      await screen.findByText("A view with this name already exists in this browser.")
    ).toBeTruthy()
  })
})
