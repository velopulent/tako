import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { type Theme, ThemeProvider, useTheme } from "./theme-provider"

let dark = false
let change: () => void = () => {}

beforeEach(() => {
  localStorage.clear()
  dark = false
  document.documentElement.className = ""
  vi.stubGlobal("matchMedia", () => ({
    matches: dark,
    addEventListener: (_: string, listener: () => void) => {
      change = listener
    },
    removeEventListener: vi.fn(),
  }))
})
afterEach(() => vi.unstubAllGlobals())

function Controls() {
  const { theme, setTheme } = useTheme()
  return (
    <button type="button" onClick={() => setTheme("system")}>
      {theme}
    </button>
  )
}
function mount() {
  return render(
    <ThemeProvider disableTransitionOnChange={false}>
      <Controls />
    </ThemeProvider>
  )
}
it("defaults to Tako and follows live system changes", () => {
  mount()
  expect(document.documentElement.className).toBe("light tako-light")
  act(() => {
    dark = true
    change()
  })
  expect(document.documentElement.className).toBe("dark tako-dark")
  expect(document.documentElement.style.colorScheme).toBe("dark")
})
it.each<Theme>(["light", "dark", "tako-light", "tako-dark"])(
  "restores %s and switches back to System",
  (theme) => {
    localStorage.setItem("tako-theme", theme)
    mount()
    expect(document.documentElement.classList.contains(theme)).toBe(true)
    fireEvent.click(screen.getByRole("button"))
    expect(localStorage.getItem("tako-theme")).toBe("system")
    expect(document.documentElement.className).toBe("light tako-light")
  }
)
it.each([
  ["system", "tako-dark"],
  ["tako-light", "tako-dark"],
  ["tako-dark", "tako-light"],
  ["light", "dark"],
  ["dark", "light"],
])("toggles %s to %s with D, but ignores typing", (theme, next) => {
  localStorage.setItem("tako-theme", theme)
  mount()
  fireEvent.keyDown(window, { key: "d" })
  expect(localStorage.getItem("tako-theme")).toBe(next)
  const input = document.createElement("input")
  document.body.append(input)
  fireEvent.keyDown(input, { key: "d" })
  expect(localStorage.getItem("tako-theme")).toBe(next)
  input.remove()
})
it("syncs other tabs and falls back to Tako for invalid preferences", () => {
  localStorage.setItem("tako-theme", "invalid")
  mount()
  expect(document.documentElement.className).toBe("light tako-light")
  fireEvent(
    window,
    new StorageEvent("storage", {
      key: "tako-theme",
      newValue: "tako-dark",
      storageArea: localStorage,
    })
  )
  expect(document.documentElement.className).toBe("dark tako-dark")
})
