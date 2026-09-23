import type { ColorToken, ResolvedTheme } from '@shared/ipc'

/**
 * Literal hex mirror of the colour custom properties in `tokens.css`, keyed by
 * resolved theme. Needed anywhere a browser API can't consume a CSS custom
 * property directly — the native `<input type="color">` picker, and Monaco's
 * theme API. Kept in step by hand, the same tradeoff `monacoSetup.ts`
 * already accepted for its own theme definitions.
 */
export const BASE_HEX: Record<ResolvedTheme, Record<ColorToken, string>> = {
  dark: {
    'bg-0': '#0b0c0e',
    'bg-1': '#101114',
    'bg-2': '#16181c',
    'bg-3': '#1d2026',
    inset: '#08090b',
    'line-1': '#1e2128',
    'line-2': '#282c34',
    'line-3': '#383e48',
    'fg-1': '#e4e7ec',
    'fg-2': '#98a0ad',
    'fg-3': '#6a727f',
    'fg-4': '#474e59',
    accent: '#4d8dfd',
    'accent-fg': '#ffffff',
    'st-idle': '#5b6472',
    'st-running': '#56d6ff',
    'st-done': '#57e8ab',
    'st-failed': '#ff6f6f',
    'st-waiting': '#f5b544',
  },
  light: {
    'bg-0': '#eef0f4',
    'bg-1': '#f7f8fa',
    'bg-2': '#eceef2',
    'bg-3': '#e2e6ec',
    inset: '#ffffff',
    'line-1': '#e3e6ec',
    'line-2': '#d4d9e1',
    'line-3': '#b9c0cb',
    'fg-1': '#14171c',
    'fg-2': '#454c57',
    'fg-3': '#5f6772',
    'fg-4': '#767e8a',
    accent: '#2563eb',
    'accent-fg': '#ffffff',
    'st-idle': '#78818e',
    'st-running': '#0a76a8',
    'st-done': '#0f7f57',
    'st-failed': '#c0362f',
    'st-waiting': '#9a6410',
  },
}

/** Weak-variant alpha percentage per status token — mirrors tokens.css's `-weak` values. */
export const WEAK_ALPHA: Partial<Record<ColorToken, number>> = {
  'st-idle': 14,
  'st-running': 13,
  'st-done': 13,
  'st-failed': 13,
  'st-waiting': 14,
}
