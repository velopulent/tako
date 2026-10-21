import { useEffect } from "react"

import { api } from "@/lib/api"

const heartbeatInterval = 60_000
const activityEvents = ["pointerdown", "keydown", "wheel", "touchstart"]

// useActivityHeartbeat tells the gateway the user is still here. Polling never
// counts as activity, so without it reading a page would end in the idle
// sign-out. It reports at most once a minute and only for visible tabs.
export function useActivityHeartbeat(enabled: boolean) {
  useEffect(() => {
    if (!enabled) return
    let last = 0
    const report = () => {
      const now = Date.now()
      if (
        document.visibilityState === "hidden" ||
        now - last < heartbeatInterval
      )
        return
      last = now
      void api("/auth/activity", { method: "POST" }).catch(() => undefined)
    }
    for (const name of activityEvents) {
      window.addEventListener(name, report, { capture: true, passive: true })
    }
    return () => {
      for (const name of activityEvents) {
        window.removeEventListener(name, report, { capture: true })
      }
    }
  }, [enabled])
}
