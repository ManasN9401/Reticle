import { loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor'
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import jsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker'
import cssWorker from 'monaco-editor/esm/vs/language/css/css.worker?worker'
import htmlWorker from 'monaco-editor/esm/vs/language/html/html.worker?worker'
import tsWorker from 'monaco-editor/esm/vs/language/typescript/ts.worker?worker'
import { BASE_HEX } from '@/design/palette'
import type { ColorScheme, ResolvedTheme } from '@shared/ipc'

/**
 * Monaco, bundled rather than fetched.
 *
 * `@monaco-editor/react` loads Monaco from a CDN by default, which would leave
 * the editor permanently blank in a packaged Electron app with no network. We
 * hand it the locally bundled copy and wire the language workers through Vite's
 * `?worker` imports instead.
 */

declare global {
  interface Window {
    MonacoEnvironment?: monaco.Environment
  }
}

window.MonacoEnvironment = {
  getWorker(_workerId: string, label: string) {
    switch (label) {
      case 'json':
        return new jsonWorker()
      case 'css':
      case 'scss':
      case 'less':
        return new cssWorker()
      case 'html':
      case 'handlebars':
      case 'razor':
        return new htmlWorker()
      case 'typescript':
      case 'javascript':
        return new tsWorker()
      default:
        return new editorWorker()
    }
  },
}

export const RETICLE_DARK = 'reticle-dark'
export const RETICLE_LIGHT = 'reticle-light'
export const RETICLE_CUSTOM = 'reticle-custom'

let configured = false

/** Syntax-highlighting rules are unrelated to the achromatic-chrome/colour-scheme system, so a custom scheme reuses whichever base's rules match its `base` field rather than inventing derived ones. */
const RULES: Record<ResolvedTheme, monaco.editor.ITokenThemeRule[]> = {
  dark: [
    { token: 'comment', foreground: '5a6472' },
    { token: 'string', foreground: '57e8ab' },
    { token: 'number', foreground: '56d6ff' },
    { token: 'keyword', foreground: '4d8dfd' },
    { token: 'type', foreground: 'f5b544' },
  ],
  light: [
    { token: 'comment', foreground: '767e8a' },
    { token: 'string', foreground: '0f7f57' },
    { token: 'number', foreground: '0a76a8' },
    { token: 'keyword', foreground: '2563eb' },
    { token: 'type', foreground: '9a6410' },
  ],
}

/**
 * Both built-in editor themes mirror the values in `src/design/tokens.css`.
 * Monaco needs literal hex rather than custom properties, so these are kept in
 * step by hand — if a surface token changes there, change it here too.
 */
export function setupMonaco(): void {
  monaco.editor.defineTheme(RETICLE_DARK, {
    base: 'vs-dark',
    inherit: true,
    rules: RULES.dark,
    colors: editorColorsFromHex(BASE_HEX.dark),
  })

  monaco.editor.defineTheme(RETICLE_LIGHT, {
    base: 'vs',
    inherit: true,
    rules: RULES.light,
    colors: editorColorsFromHex(BASE_HEX.light),
  })

  if (!configured) {
    configured = true
    loader.config({ monaco })
  }
}

/**
 * Defines (or redefines) the `reticle-custom` Monaco theme from an active
 * colour scheme, layering its overrides on top of the literal hex for
 * whichever base it was built on. Call this whenever the active scheme
 * changes, before setting the editor's theme to `RETICLE_CUSTOM`.
 */
export function defineCustomMonacoTheme(scheme: ColorScheme): void {
  const hex = { ...BASE_HEX[scheme.base], ...scheme.tokens }
  monaco.editor.defineTheme(RETICLE_CUSTOM, {
    base: scheme.base === 'light' ? 'vs' : 'vs-dark',
    inherit: true,
    rules: RULES[scheme.base],
    colors: editorColorsFromHex(hex),
  })
}

function editorColorsFromHex(hex: Record<string, string>): Record<string, string> {
  return editorColors(
    hex.inset,
    hex['fg-1'],
    hex['fg-4'],
    hex['fg-2'],
    hex['bg-1'],
    hex['line-1'],
    hex['bg-2'],
    hex['line-2'],
  )
}

function editorColors(
  inset: string,
  fg1: string,
  fg4: string,
  fg2: string,
  bg1: string,
  line1: string,
  bg2: string,
  line2: string,
): Record<string, string> {
  return {
    'editor.background': inset,
    'editor.foreground': fg1,
    'editorLineNumber.foreground': fg4,
    'editorLineNumber.activeForeground': fg2,
    'editor.lineHighlightBackground': bg1,
    'editorGutter.background': inset,
    'editorIndentGuide.background1': line1,
    'editorWidget.background': bg2,
    'editorWidget.border': line2,
  }
}

/** Map a filename to a Monaco language id. */
export function languageFor(path: string): string {
  const ext = path.slice(path.lastIndexOf('.') + 1).toLowerCase()
  switch (ext) {
    case 'ts':
    case 'tsx':
      return 'typescript'
    case 'js':
    case 'jsx':
    case 'mjs':
    case 'cjs':
      return 'javascript'
    case 'py':
      return 'python'
    case 'go':
      return 'go'
    case 'json':
      return 'json'
    case 'yaml':
    case 'yml':
      return 'yaml'
    case 'md':
      return 'markdown'
    case 'css':
      return 'css'
    case 'html':
      return 'html'
    case 'sh':
    case 'bash':
      return 'shell'
    case 'toml':
      return 'ini'
    case 'sql':
      return 'sql'
    default:
      return 'plaintext'
  }
}
