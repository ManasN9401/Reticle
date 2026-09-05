import type { ReticleBridge } from '@shared/ipc'

/**
 * Access to the preload bridge.
 *
 * The previous build failed silently here: `main.ts` pointed at `preload.mjs`
 * while the bundler emitted `preload.js`, so `window.electronAPI` was undefined
 * and every window control was a no-op with no error anywhere. Rather than
 * optional-chaining that class of bug into invisibility again, missing bridge
 * is treated as a hard, visible failure.
 */

export const bridge = window.reticle

export const bridgeAvailable = Boolean(bridge)

export function requireBridge(): ReticleBridge {
  if (!bridge) {
    throw new Error(
      'Preload bridge unavailable — window.reticle is undefined. The preload script failed to load.',
    )
  }
  return bridge
}
