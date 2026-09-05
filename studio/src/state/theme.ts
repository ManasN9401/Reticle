import { useEffect } from 'react'
import type { ResolvedTheme, ThemePreference } from '@shared/ipc'
import { bridge } from './bridge'
import { useStudio } from './store'

/**
 * Theme resolution.
 *
 * "system" is resolved to a concrete `dark`/`light` here rather than in CSS, so
 * the stylesheet only ever has to express two palettes and every consumer that
 * needs to know the *actual* theme (Monaco, the Electron window background) can
 * read one value.
 */

const QUERY = '(prefers-color-scheme: light)'

export function systemTheme(): ResolvedTheme {
  return typeof window !== 'undefined' && window.matchMedia(QUERY).matches
    ? 'light'
    : 'dark'
}

export function resolveTheme(preference: ThemePreference): ResolvedTheme {
  return preference === 'system' ? systemTheme() : preference
}

/** The theme actually in effect, re-evaluated when the OS setting changes. */
export function useResolvedTheme(): ResolvedTheme {
  const preference = useStudio((s) => s.settings?.appearance.theme ?? 'dark')
  const systemIsLight = useStudio((s) => s.systemPrefersLight)
  if (preference !== 'system') return preference
  return systemIsLight ? 'light' : 'dark'
}

/**
 * Applies the resolved theme to the document and tells the main process, so the
 * native window background matches and there is no flash on the next launch.
 */
export function useApplyTheme(): void {
  const theme = useResolvedTheme()
  const setSystemPrefersLight = useStudio((s) => s.setSystemPrefersLight)

  useEffect(() => {
    const media = window.matchMedia(QUERY)
    const sync = () => setSystemPrefersLight(media.matches)
    sync()
    media.addEventListener('change', sync)
    return () => media.removeEventListener('change', sync)
  }, [setSystemPrefersLight])

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    void bridge?.setThemeBackground(theme)
  }, [theme])
}

/** Cycle order for the quick toggle: what you see is what you get, then auto. */
export function nextTheme(current: ThemePreference): ThemePreference {
  switch (current) {
    case 'dark':
      return 'light'
    case 'light':
      return 'system'
    default:
      return 'dark'
  }
}
