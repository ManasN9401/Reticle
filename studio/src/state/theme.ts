import { useEffect } from 'react'
import type { ColorScheme, ResolvedTheme, ThemePreference } from '@shared/ipc'
import { isSafeColorValue } from '@shared/ipc'
import { WEAK_ALPHA } from '@/design/palette'
import { bridge } from './bridge'
import { useStudio } from './store'

/**
 * Theme resolution.
 *
 * "system" is resolved to a concrete `dark`/`light` here rather than in CSS, so
 * the stylesheet only ever has to express two palettes and every consumer that
 * needs to know the *actual* theme (Monaco, the Electron window background) can
 * read one value. "custom" resolves to whichever base (dark/light) the active
 * colour scheme was built on — that base still supplies every token the scheme
 * doesn't override.
 */

const QUERY = '(prefers-color-scheme: light)'

export function systemTheme(): ResolvedTheme {
  return typeof window !== 'undefined' && window.matchMedia(QUERY).matches
    ? 'light'
    : 'dark'
}

export function resolveTheme(preference: ThemePreference, customBase: ResolvedTheme = 'dark'): ResolvedTheme {
  if (preference === 'system') return systemTheme()
  if (preference === 'custom') return customBase
  return preference
}

/** The custom scheme `appearance.theme: 'custom'` currently applies, if any. */
export function useActiveColorScheme(): ColorScheme | null {
  return useStudio((s) => {
    const colorSchemes = s.settings?.colorSchemes
    if (!colorSchemes?.activeId) return null
    return colorSchemes.schemes.find((scheme) => scheme.id === colorSchemes.activeId) ?? null
  })
}

/** The theme actually in effect, re-evaluated when the OS setting or active scheme changes. */
export function useResolvedTheme(): ResolvedTheme {
  const preference = useStudio((s) => s.settings?.appearance.theme ?? 'dark')
  const systemIsLight = useStudio((s) => s.systemPrefersLight)
  const activeScheme = useActiveColorScheme()
  if (preference === 'custom') return activeScheme?.base ?? 'dark'
  if (preference !== 'system') return preference
  return systemIsLight ? 'light' : 'dark'
}

const CUSTOM_STYLE_ID = 'reticle-custom-theme'

/**
 * Injects a `:root` block overriding only the tokens the active scheme
 * customizes. A later `<style>` tag wins the cascade at equal specificity, so
 * this always takes priority over the base dark/light palette without needing
 * to match `[data-theme]` itself. Every value is re-validated here too, in
 * case settings.json was hand-edited outside the settings UI's own checks.
 */
function applyCustomScheme(scheme: ColorScheme | null): void {
  let style = document.getElementById(CUSTOM_STYLE_ID) as HTMLStyleElement | null
  if (!scheme || Object.keys(scheme.tokens).length === 0) {
    style?.remove()
    return
  }
  if (!style) {
    style = document.createElement('style')
    style.id = CUSTOM_STYLE_ID
    document.head.appendChild(style)
  }
  const declarations: string[] = []
  for (const [token, value] of Object.entries(scheme.tokens)) {
    if (!value || !isSafeColorValue(value)) continue
    declarations.push(`--color-${token}:${value};`)
    const alpha = WEAK_ALPHA[token as keyof typeof WEAK_ALPHA]
    if (alpha !== undefined) {
      declarations.push(`--color-${token}-weak:color-mix(in srgb, ${value} ${alpha}%, transparent);`)
    }
  }
  style.textContent = `:root{${declarations.join('')}}`
}

/**
 * Applies the resolved theme to the document and tells the main process, so the
 * native window background matches and there is no flash on the next launch.
 */
export function useApplyTheme(): void {
  const theme = useResolvedTheme()
  const preference = useStudio((s) => s.settings?.appearance.theme ?? 'dark')
  const activeScheme = useActiveColorScheme()
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

  useEffect(() => {
    applyCustomScheme(preference === 'custom' ? activeScheme : null)
  }, [preference, activeScheme])
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
