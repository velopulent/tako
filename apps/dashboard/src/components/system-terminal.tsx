import { FitAddon } from "@xterm/addon-fit"
import { Terminal } from "@xterm/xterm"
import * as React from "react"
import "@xterm/xterm/css/xterm.css"

export function SystemTerminal() {
  const container = React.useRef<HTMLDivElement>(null)
  const [state, setState] = React.useState("Connecting…")

  React.useEffect(() => {
    if (!container.current) return

    const terminal = new Terminal({
      convertEol: true,
      cursorBlink: true,
      fontFamily:
        "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
      fontSize: 14,
      scrollback: 5000,
    })
    const applyTheme = () => {
      const styles = getComputedStyle(document.documentElement)
      const token = (name: string) => styles.getPropertyValue(name).trim()
      terminal.options.theme = {
        background: token("--terminal"),
        foreground: token("--terminal-foreground"),
        cursor: token("--terminal-foreground"),
        selectionBackground: token("--terminal-selection"),
      }
    }
    applyTheme()
    const themeObserver = new MutationObserver(applyTheme)
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(container.current)
    fit.fit()

    const scheme = window.location.protocol === "https:" ? "wss:" : "ws:"
    const socket = new WebSocket(
      `${scheme}//${window.location.host}/api/v1/terminal/ws?columns=${terminal.cols}&rows=${terminal.rows}`
    )
    socket.binaryType = "arraybuffer"
    socket.onopen = () => {
      setState("Connected")
      terminal.focus()
    }
    socket.onmessage = (event) =>
      terminal.write(
        event.data instanceof ArrayBuffer
          ? new Uint8Array(event.data)
          : event.data
      )
    socket.onerror = () => setState("Connection failed")
    socket.onclose = (event) => {
      setState(event.reason || "Disconnected")
      terminal.write("\r\n\x1b[2m[session closed]\x1b[0m\r\n")
    }
    const input = terminal.onData((data) => {
      if (socket.readyState === WebSocket.OPEN)
        socket.send(new TextEncoder().encode(data))
    })
    const resize = terminal.onResize(({ cols, rows }) => {
      if (socket.readyState === WebSocket.OPEN)
        socket.send(JSON.stringify({ columns: cols, rows }))
    })
    const observer = new ResizeObserver(() => fit.fit())
    observer.observe(container.current)

    return () => {
      themeObserver.disconnect()
      observer.disconnect()
      input.dispose()
      resize.dispose()
      socket.close(1000, "page closed")
      terminal.dispose()
    }
  }, [])

  return (
    <div className="overflow-hidden rounded-xl border border-terminal-border bg-terminal text-terminal-foreground shadow-sm">
      <div className="flex items-center justify-between border-b border-terminal-border px-4 py-2 text-xs text-terminal-muted">
        <span>Authenticated shell</span>
        <span aria-live="polite">{state}</span>
      </div>
      <div
        ref={container}
        className="h-[65vh] min-h-80 p-3"
        role="application"
        aria-label="Interactive terminal"
      />
    </div>
  )
}
