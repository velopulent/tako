import { act, render } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"

import { ThemeProvider, useTheme } from "@/components/theme-provider"

let prefersDark = false

beforeEach(() => {
  prefersDark = false
  localStorage.clear()
  document.documentElement.className = ""
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: prefersDark,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }))
  )
})

afterEach(() => vi.unstubAllGlobals())

function Probe({
  onReady,
}: {
  onReady: (value: ReturnType<typeof useTheme>) => void
}) {
  onReady(useTheme())
  return null
}

function mount() {
  let current: ReturnType<typeof useTheme> | undefined
  render(
    <ThemeProvider>
      <Probe onReady={(value) => (current = value)} />
    </ThemeProvider>
  )
  return () => current as ReturnType<typeof useTheme>
}

it("follows the system preference by default", () => {
  prefersDark = true
  const theme = mount()
  expect(theme().theme).toBe("system")
  expect(document.documentElement.classList.contains("dark")).toBe(true)
})

it("stores an explicit choice and applies it", () => {
  const theme = mount()
  act(() => theme().setTheme("dark"))
  expect(localStorage.getItem("tako-theme")).toBe("dark")
  expect(document.documentElement.classList.contains("dark")).toBe(true)
  act(() => theme().setTheme("light"))
  expect(document.documentElement.classList.contains("dark")).toBe(false)
})

it("ignores unknown stored values", () => {
  localStorage.setItem("tako-theme", "tako-dark")
  const theme = mount()
  expect(theme().theme).toBe("system")
})
