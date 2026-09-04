import { fireEvent, render, screen } from "@testing-library/react"
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
  it("renders the hostname and fades in a loaded distro background", () => {
    render(
      <LoginPage
        branding={{
          distribution: "ubuntu",
          hostname: "server-01",
          backgroundUrl: "/branding/ubuntu.png?v=1.2.3",
        }}
        onAuthenticated={vi.fn()}
      />
    )

    expect(screen.getByText("Login to server-01")).toBeTruthy()
    const image = document.querySelector("main img")
    expect(image).not.toBeNull()
    expect(image?.classList.contains("opacity-0")).toBe(true)

    fireEvent.load(image as HTMLImageElement)
    expect(image?.classList.contains("opacity-100")).toBe(true)
  })

  it("keeps the neutral backdrop when the background image fails", () => {
    render(
      <LoginPage
        branding={{
          distribution: "fedora",
          hostname: "server-02",
          backgroundUrl: "/branding/fedora.png?v=1.2.3",
        }}
        onAuthenticated={vi.fn()}
      />
    )

    const image = document.querySelector("main img")
    expect(image).not.toBeNull()
    fireEvent.error(image as HTMLImageElement)
    expect(image?.classList.contains("opacity-0")).toBe(true)
  })

  it("shows the login form while the session is checked", () => {
    render(
      <LoginPage
        branding={{ distribution: "debian", hostname: "server-03" }}
        onAuthenticated={vi.fn()}
      />
    )

    expect(screen.getByText("Login to server-03")).toBeTruthy()
    expect(screen.getByLabelText("Username")).toBeTruthy()
    expect(screen.queryByRole("status")).toBeNull()
  })

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

  it("does not treat a session setup failure as wrong credentials", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(
            { code: "session-failed", detail: "Could not create session" },
            500
          )
        )
    )
    const user = userEvent.setup()
    render(<LoginPage onAuthenticated={vi.fn()} />)

    await user.type(screen.getByLabelText("Username"), "krishna")
    await user.type(screen.getByLabelText("Password"), "secret{Enter}")

    expect(
      await screen.findByText(
        "Signed in, but the user session could not start. Check tako-sessiond on the host."
      )
    ).toBeTruthy()
    expect(
      screen.queryByText("Check your username and password, then try again.")
    ).toBeNull()
  })
})
