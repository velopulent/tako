import { FitAddon } from "@xterm/addon-fit"
import { Terminal } from "@xterm/xterm"
import { RotateCcwIcon } from "lucide-react"
import * as React from "react"
import "@xterm/xterm/css/xterm.css"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

type Connection = "connecting" | "connected" | "closed"

export function SystemTerminal() {
  const container = React.useRef<HTMLDivElement>(null)
  const [connection, setConnection] = React.useState<Connection>("connecting")
  const [reason, setReason] = React.useState("")
  const [generation, setGeneration] = React.useState(0)

  // biome-ignore lint/correctness/useExhaustiveDependencies: generation reconnects on demand
  React.useEffect(() => {
    const element = container.current
    if (!element) return
    setConnection("connecting")
    setReason("")
    const terminal = new Terminal({
      cursorBlink: true,
      fontFamily:
        "ui-monospace, 'Cascadia Code', 'JetBrains Mono', Menlo, Consolas, monospace",
      fontSize: 13.5,
      lineHeight: 1.15,
      scrollback: 10_000,
      allowProposedApi: false,
    })
    const styles = getComputedStyle(document.documentElement)
    const token = (name: string) => styles.getPropertyValue(name).trim()
    terminal.options.theme = {
      background: token("--terminal"),
      foreground: token("--terminal-foreground"),
      cursor: token("--terminal-foreground"),
      selectionBackground: token("--terminal-selection"),
    }
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(element)
    fit.fit()

    const scheme = window.location.protocol === "https:" ? "wss:" : "ws:"
    const socket = new WebSocket(
      `${scheme}//${window.location.host}/api/v1/terminal/ws?columns=${terminal.cols}&rows=${terminal.rows}`
    )
    socket.binaryType = "arraybuffer"
    socket.onopen = () => {
      setConnection("connected")
      terminal.focus()
    }
    socket.onmessage = (event) =>
      terminal.write(
        event.data instanceof ArrayBuffer
          ? new Uint8Array(event.data)
          : event.data
      )
    socket.onclose = (event) => {
      setConnection("closed")
      setReason(
        event.reason && event.reason !== "terminal closed" ? event.reason : ""
      )
      terminal.write("\r\n\x1b[2m[session ended]\x1b[0m\r\n")
    }
    const encoder = new TextEncoder()
    const input = terminal.onData(
      (data) =>
        socket.readyState === WebSocket.OPEN &&
        socket.send(encoder.encode(data))
    )
    const resize = terminal.onResize(
      ({ cols, rows }) =>
        socket.readyState === WebSocket.OPEN &&
        socket.send(JSON.stringify({ columns: cols, rows }))
    )
    let frame = 0
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => fit.fit())
    })
    observer.observe(element)
    return () => {
      cancelAnimationFrame(frame)
      observer.disconnect()
      input.dispose()
      resize.dispose()
      socket.close(1000, "page closed")
      terminal.dispose()
    }
  }, [generation])

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-terminal-border bg-terminal text-terminal-foreground">
      <div className="flex h-10 shrink-0 items-center justify-between gap-3 border-b border-terminal-border px-3 text-xs text-terminal-muted">
        <span className="flex items-center gap-2">
          <span
            className={cn(
              "size-2 rounded-full",
              connection === "connected"
                ? "bg-success"
                : connection === "connecting"
                  ? "animate-pulse bg-warning"
                  : "bg-terminal-muted"
            )}
            aria-hidden="true"
          />
          <span aria-live="polite">
            {connection === "connected"
              ? "Connected"
              : connection === "connecting"
                ? "Connecting…"
                : reason || "Session ended"}
          </span>
        </span>
        {connection === "closed" && (
          <Button
            size="xs"
            variant="ghost"
            className="text-terminal-foreground hover:bg-white/10 hover:text-terminal-foreground"
            onClick={() => setGeneration((value) => value + 1)}
          >
            <RotateCcwIcon data-icon="inline-start" />
            New session
          </Button>
        )}
      </div>
      <div
        ref={container}
        className="min-h-80 flex-1 px-3 py-2"
        role="application"
        aria-label="Terminal"
      />
    </div>
  )
}
