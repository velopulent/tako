import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { LoginPage } from "@/components/login-page"

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("LoginPage", () => {
  it("completes a keyboard-driven multi-round PAM conversation", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse(
          {
            conversationId: "conversation-token",
            prompts: [
              { id: "otp", style: "text", message: "Verification code:" },
            ],
          },
          202
        )
      )
      .mockResolvedValueOnce(
        jsonResponse({
          user: { username: "octopus", name: "Octopus", uid: 1000, gid: 1000 },
          csrfToken: "csrf-token",
        })
      )
    vi.stubGlobal("fetch", fetchMock)
    const authenticated = vi.fn()
    const user = userEvent.setup()
    render(<LoginPage onAuthenticated={authenticated} />)

    await user.type(screen.getByLabelText("Username"), "octopus")
    await user.type(screen.getByLabelText("Password"), "secret{Enter}")

    const code = await screen.findByLabelText("Verification code:")
    expect(screen.queryByLabelText("Password")).toBeNull()
    await user.type(code, "123456{Enter}")

    await vi.waitFor(() => expect(authenticated).toHaveBeenCalledTimes(1))
    expect(fetchMock.mock.calls[1]?.[1]?.body).toBe(
      JSON.stringify({
        conversationId: "conversation-token",
        responses: [{ id: "otp", value: "123456" }],
      })
    )
  })

  it("cancels a pending conversation and clears its prompt", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse(
          {
            conversationId: "conversation-token",
            prompts: [
              { id: "notice", style: "info", message: "Touch your key" },
            ],
          },
          202
        )
      )
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<LoginPage onAuthenticated={vi.fn()} />)

    await user.type(screen.getByLabelText("Username"), "octopus")
    await user.type(screen.getByLabelText("Password"), "secret{Enter}")
    await screen.findByText("Touch your key")
    await user.click(screen.getByRole("button", { name: "Start over" }))

    expect(await screen.findByLabelText("Username")).toBeTruthy()
    await vi.waitFor(() => {
      expect(fetchMock.mock.calls[1]?.[1]?.body).toBe(
        JSON.stringify({ conversationId: "conversation-token", cancel: true })
      )
    })
  })
})
