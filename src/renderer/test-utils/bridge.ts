export function installContextBridgeLike(props: Record<string, unknown>): void {
  const bridge = {}
  for (const [key, value] of Object.entries(props)) {
    Object.defineProperty(bridge, key, {
      value,
      writable: false,
      configurable: false,
      enumerable: true,
    })
  }
  Object.defineProperty(window, 'muesli', {
    value: bridge,
    writable: false,
    configurable: true, // configurable so tests can reinstall between cases
    enumerable: true,
  })
}
