import { useQuery } from "@tanstack/react-query"
import { APIError, api, type SessionResponse, setCSRFToken } from "@/lib/api"

export const sessionKey = ["session"] as const

export async function loadSession() {
  try {
    const session = await api<SessionResponse>("/auth/session")
    setCSRFToken(session.csrfToken)
    return session
  } catch (error) {
    if (error instanceof APIError && error.status === 401) return null
    throw error
  }
}

// useSession returns the signed-in session. Inside the authenticated app it is
// always present; it refreshes so administrative expiry shows up promptly.
export function useSession() {
  const query = useQuery({
    queryKey: sessionKey,
    queryFn: loadSession,
    refetchInterval: 30_000,
  })
  return query.data as SessionResponse
}

export function isAdministrative(session: SessionResponse | null | undefined) {
  return Boolean(
    session?.administrative &&
      session.adminUntil &&
      new Date(session.adminUntil).getTime() > Date.now()
  )
}
