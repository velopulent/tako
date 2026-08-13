# ADR 0002: API and stream transports

Status: accepted

Snapshot queries and actions use a versioned JSON HTTP API under `/api/v1`. One-way telemetry and log following use SSE because browser reconnection and cancellation are sufficient. Interactive terminal traffic uses a dedicated WebSocket because input, output, resize, and exit events are bidirectional.

Module RPC traffic uses four-byte big-endian length-prefixed JSON frames. Frames have request IDs, methods, payloads, and stable error codes. Messages are capped at 1 MiB. Terminal PTY bytes use a dedicated raw local stream; resize controls use narrow authenticated JSON requests so binary shell traffic is never re-encoded.
