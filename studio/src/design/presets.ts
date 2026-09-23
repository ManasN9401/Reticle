import type { ColorScheme } from '@shared/ipc'

/**
 * Built-in colour scheme starting points. Not a parallel persisted concept —
 * "Start from a preset" copies one of these into a normal user-owned
 * `ColorScheme` (fresh id) and applies it, exactly like "New scheme" does.
 * Chosen for concrete accessibility reasons, since status colour is this
 * app's only status signal — not for arbitrary aesthetic variety.
 */
export const PRESET_COLOR_SCHEMES: Omit<ColorScheme, 'id'>[] = [
  {
    name: 'High Contrast',
    base: 'dark',
    tokens: {
      'bg-0': '#000000',
      'bg-1': '#0a0a0a',
      'bg-2': '#141414',
      'bg-3': '#1e1e1e',
      inset: '#000000',
      'line-2': '#555555',
      'line-3': '#777777',
      'fg-1': '#ffffff',
      'fg-2': '#e0e0e0',
      accent: '#66aaff',
      'st-idle': '#999999',
      'st-running': '#00e5ff',
      'st-done': '#00ff8c',
      'st-failed': '#ff3b30',
      'st-waiting': '#ffd60a',
    },
  },
  {
    // Okabe & Ito (2008) palette: chosen so running/done/failed/waiting stay
    // distinguishable under the common forms of colour vision deficiency.
    name: 'Colourblind Safe',
    base: 'dark',
    tokens: {
      accent: '#CC79A7',
      'st-idle': '#8a8a8a',
      'st-running': '#0072B2',
      'st-done': '#009E73',
      'st-failed': '#D55E00',
      'st-waiting': '#E69F00',
    },
  },
]
