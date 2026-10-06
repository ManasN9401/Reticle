/**
 * ANSI colours for the integrated terminal, one set per theme.
 *
 * A single dark palette was used for both themes, so on the light theme the
 * "white", bright and yellow colours that programs print for emphasis nearly
 * vanished. These are chosen to stay readable on each theme's inset colour.
 */
export type TerminalThemeName = 'dark' | 'light'

export interface AnsiPalette {
  black: string
  red: string
  green: string
  yellow: string
  blue: string
  magenta: string
  cyan: string
  white: string
  brightBlack: string
  brightRed: string
  brightGreen: string
  brightYellow: string
  brightBlue: string
  brightMagenta: string
  brightCyan: string
  brightWhite: string
}

export const ANSI_PALETTES: Record<TerminalThemeName, AnsiPalette> = {
  dark: {
    black: '#17191d', red: '#e06c75', green: '#98c379', yellow: '#e5c07b',
    blue: '#61afef', magenta: '#c678dd', cyan: '#56b6c2', white: '#d7dae0',
    brightBlack: '#5c6370', brightRed: '#ef7a85', brightGreen: '#a9d58a', brightYellow: '#f0cf8d',
    brightBlue: '#7bbcff', brightMagenta: '#d68ced', brightCyan: '#6bc7d3', brightWhite: '#f2f4f8',
  },
  light: {
    black: '#1f2328', red: '#cf222e', green: '#116329', yellow: '#7d4e00',
    blue: '#0550ae', magenta: '#8250df', cyan: '#1b7c83', white: '#57606a',
    brightBlack: '#6e7781', brightRed: '#a40e26', brightGreen: '#1a7f37', brightYellow: '#9a6700',
    brightBlue: '#0969da', brightMagenta: '#6639ba', brightCyan: '#0e6f76', brightWhite: '#424a53',
  },
}
