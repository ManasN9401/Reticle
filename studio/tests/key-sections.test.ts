import test from 'node:test'
import assert from 'node:assert/strict'
import { buildKeySections } from '../src/features/settings/keySections'
import type { EnvKeyEntry, ProviderStatus } from '../src/shared/ipc'

const keys: EnvKeyEntry[] = [
  { name: 'OPENROUTER_API_KEY', provider: 'openrouter', slotLabel: 'Primary', known: true, present: true, masked: '••••tail', length: 12 },
  { name: 'COHERE_API_KEY', provider: 'other', known: false, present: true, masked: '••••tail', length: 12 },
  { name: 'UNASSIGNED_TOKEN', provider: 'other', known: false, present: true, masked: '••••tail', length: 12 },
]

const profiles: ProviderStatus[] = [
  {
    config: {
      id: 'cohere',
      name: 'Cohere',
      protocol: 'openai-compatible',
      baseUrl: 'https://example.invalid/v1',
      apiKeyEnv: 'COHERE_API_KEY',
      enabled: true,
    },
    state: 'ready',
    modelCount: 1,
  },
  {
    config: {
      id: 'llm7',
      name: 'LLM7',
      protocol: 'openai-compatible',
      baseUrl: 'https://example.invalid/v1',
      apiKeyEnv: 'LLM7_API_KEY',
      enabled: true,
    },
    state: 'missing_credential',
    modelCount: 0,
  },
]

test('profile keys appear under provider headings, including missing variables', () => {
  const sections = buildKeySections(keys, profiles)

  assert.deepEqual(sections.map((section) => section.label), [
    'OpenRouter',
    'Cohere',
    'LLM7',
    'Other variables',
  ])
  assert.equal(sections[1].entries[0].name, 'COHERE_API_KEY')
  assert.equal(sections[1].entries[0].known, true)
  assert.equal(sections[2].entries[0].name, 'LLM7_API_KEY')
  assert.equal(sections[2].entries[0].present, false)
  assert.deepEqual(sections[3].entries.map((entry) => entry.name), ['UNASSIGNED_TOKEN'])
})
