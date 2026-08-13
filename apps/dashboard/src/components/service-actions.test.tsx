import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { ServiceActions } from "@/components/service-actions"

describe("ServiceActions", () => {
  it("requires administrative access for system units", () => {
    render(
      <ServiceActions
        scope="system"
        unit="demo.service"
        administrative={false}
        pending={false}
        onAction={vi.fn()}
      />
    )
    expect(
      (screen.getByRole("button", { name: "Restart" }) as HTMLButtonElement)
        .disabled
    ).toBe(true)
  })

  it("confirms a user action before invoking the callback", async () => {
    const onAction = vi.fn()
    const user = userEvent.setup()
    render(
      <ServiceActions
        scope="user"
        unit="demo.service"
        administrative={false}
        pending={false}
        onAction={onAction}
      />
    )
    await user.click(screen.getByRole("button", { name: "Restart" }))
    expect(screen.getByText("restart demo.service?")).toBeTruthy()
    await user.click(screen.getByRole("button", { name: "Confirm" }))
    expect(onAction).toHaveBeenCalledWith("restart")
  })
})
