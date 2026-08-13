export const serviceActionNames = [
  "start",
  "stop",
  "restart",
  "reload",
  "enable",
  "disable",
  "mask",
  "unmask",
] as const

export type ServiceActionName = (typeof serviceActionNames)[number]
