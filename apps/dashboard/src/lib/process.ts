const stateNames: Record<string, string> = {
  R: "Running",
  S: "Sleeping",
  D: "Waiting on I/O",
  I: "Idle",
  T: "Stopped",
  t: "Traced",
  Z: "Zombie",
  X: "Dead",
}

// processStateName names a /proc state letter.
export function processStateName(state: string) {
  return stateNames[state] ?? state
}
