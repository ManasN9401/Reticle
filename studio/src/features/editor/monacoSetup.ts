import { loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor'
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import jsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker'
import cssWorker from 'monaco-editor/esm/vs/language/css/css.worker?worker'
import htmlWorker from 'monaco-editor/esm/vs/language/html/html.worker?worker'
import tsWorker from 'monaco-editor/esm/vs/language/typescript/ts.worker?worker'

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

export const RETICLE_THEME = 'reticle-dark'

/**
 * Editor colours are read from the token layer at runtime so the editor cannot
 * drift from the rest of the app. Monaco needs literal hex, hence the probe.
 */
function token(name: string, fallback: string): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

let configured = false

export function setupMonaco(): void {
  if (configured) return
  configured = true

  monaco.editor.defineTheme(RETICLE_THEME, {
    base: 'vs-dark',
    inherit: true,
    rules: [
      { token: 'comment', foreground: token('--color-fg-4', '#474e59').slice(1) },
      { token: 'string', foreground: '57e8ab' },
      { token: 'number', foreground: '56d6ff' },
      { token: 'keyword', foreground: '4d8dfd' },
      { token: 'type', foreground: 'f5b544' },
    ],
    colors: {
      'editor.background': token('--color-inset', '#08090b'),
      'editor.foreground': token('--color-fg-1', '#e4e7ec'),
      'editorLineNumber.foreground': token('--color-fg-4', '#474e59'),
      'editorLineNumber.activeForeground': token('--color-fg-2', '#98a0ad'),
      'editor.lineHighlightBackground': token('--color-bg-1', '#101114'),
      'editorGutter.background': token('--color-inset', '#08090b'),
      'editorIndentGuide.background1': token('--color-line-1', '#1e2128'),
      'editorWidget.background': token('--color-bg-2', '#16181c'),
      'editorWidget.border': token('--color-line-2', '#282c34'),
      'scrollbarSlider.background': '#282c3488',
      'scrollbarSlider.hoverBackground': '#383e48aa',
    },
  })

  loader.config({ monaco })
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
