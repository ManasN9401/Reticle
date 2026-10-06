import test from 'node:test'
import assert from 'node:assert/strict'
import { ANSI_PALETTES } from '../src/features/terminal/terminalPalette'
import type { TerminalThemeName } from '../src/features/terminal/terminalPalette'

function luminance(hex: string): number {
  const channels = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
  const [r, g, b] = channels.map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

// The terminal background is the theme's --color-inset token.
const GROUND: Record<TerminalThemeName, string> = { dark: '#08090b', light: '#ffffff' }
// Black on the dark theme and white on the light one are the ground's own colour
// family, so programs use them for deliberately subdued text.
const SUBDUED: Record<TerminalThemeName, string> = { dark: 'black', light: 'white' }

test('every ANSI colour stays readable on its own theme', () => {
  for (const name of ['dark', 'light'] as const) {
    for (const [colour, value] of Object.entries(ANSI_PALETTES[name])) {
      if (colour === SUBDUED[name]) continue
      assert.ok(contrast(value, GROUND[name]) >= 3, `${name} ${colour} ${value} has contrast ${contrast(value, GROUND[name]).toFixed(2)}`)
    }
  }
})

test('both palettes define all sixteen colours', () => {
  assert.deepEqual(Object.keys(ANSI_PALETTES.dark).sort(), Object.keys(ANSI_PALETTES.light).sort())
  assert.equal(Object.keys(ANSI_PALETTES.dark).length, 16)
  for (const palette of Object.values(ANSI_PALETTES)) {
    for (const value of Object.values(palette)) assert.match(value, /^#[0-9a-f]{6}$/)
  }
})

test('the light palette is not a copy of the dark one', () => {
  assert.notEqual(ANSI_PALETTES.light.yellow, ANSI_PALETTES.dark.yellow)
  assert.notEqual(ANSI_PALETTES.light.brightBlack, ANSI_PALETTES.dark.brightBlack)
})
