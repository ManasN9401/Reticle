import type { EnvKeyEntry, KeyProvider, ProviderStatus } from '../../shared/ipc'

export interface KeySection {
  id: string
  label: string
  description?: string
  entries: EnvKeyEntry[]
}

const BUILTIN_LABELS: Record<Exclude<KeyProvider, 'other'>, string> = {
  openrouter: 'OpenRouter',
  groq: 'Groq',
  gemini: 'Gemini',
}

const BUILTIN_ORDER: Exclude<KeyProvider, 'other'>[] = ['openrouter', 'groq', 'gemini']

/**
 * Places each key under the provider that consumes it. Configurable profiles
 * turn their apiKeyEnv into a recognised slot and create an empty row when the
 * variable has not been added to .env yet.
 */
export function buildKeySections(
  keys: EnvKeyEntry[],
  profiles: ProviderStatus[],
): KeySection[] {
  const entriesByName = new Map(keys.map((entry) => [entry.name, { ...entry }]))

  for (const profile of profiles) {
    const name = profile.config.apiKeyEnv?.trim()
    if (!name) continue
    const existing = entriesByName.get(name)
    if (existing) {
      entriesByName.set(name, { ...existing, known: true })
    } else {
      entriesByName.set(name, {
        name,
        provider: 'other',
        known: true,
        present: false,
        masked: '',
        length: 0,
      })
    }
  }

  const entries = Array.from(entriesByName.values())
  const sections: KeySection[] = BUILTIN_ORDER.map((provider) => ({
    id: provider,
    label: BUILTIN_LABELS[provider],
    entries: entries.filter((entry) => entry.provider === provider),
  })).filter((section) => section.entries.length > 0)

  const assignedCustomKeys = new Set<string>()
  for (const profile of profiles) {
    const name = profile.config.apiKeyEnv?.trim()
    if (!name || assignedCustomKeys.has(name)) continue
    const entry = entriesByName.get(name)
    // Built-in slots retain their familiar built-in heading even if a custom
    // profile happens to reuse one.
    if (!entry || entry.provider !== 'other') continue
    assignedCustomKeys.add(name)
    sections.push({
      id: `profile:${profile.config.id}`,
      label: profile.config.name?.trim() || profile.config.id,
      description: 'provider profile',
      entries: [entry],
    })
  }

  const unassigned = entries.filter(
    (entry) => entry.provider === 'other' && !assignedCustomKeys.has(entry.name),
  )
  if (unassigned.length > 0) {
    sections.push({
      id: 'other',
      label: 'Other variables',
      description: 'not assigned to a provider profile',
      entries: unassigned,
    })
  }

  return sections
}
