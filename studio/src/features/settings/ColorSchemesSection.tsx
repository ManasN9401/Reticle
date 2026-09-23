import { useState } from 'react'
import { Download, Palette, Plus, Sparkles, Trash2, Upload } from 'lucide-react'
import { cn } from '@/design/cn'
import { Button, IconButton, Input } from '@/design/primitives'
import { BASE_HEX } from '@/design/palette'
import { PRESET_COLOR_SCHEMES } from '@/design/presets'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import { useResolvedTheme } from '@/state/theme'
import type { ColorScheme, ColorToken, SettingsPatch } from '@shared/ipc'

const TOKEN_GROUPS: { label: string; tokens: ColorToken[] }[] = [
  { label: 'Surfaces', tokens: ['bg-0', 'bg-1', 'bg-2', 'bg-3', 'inset'] },
  { label: 'Lines', tokens: ['line-1', 'line-2', 'line-3'] },
  { label: 'Text', tokens: ['fg-1', 'fg-2', 'fg-3', 'fg-4'] },
  { label: 'Accent', tokens: ['accent', 'accent-fg'] },
  { label: 'Status', tokens: ['st-idle', 'st-running', 'st-done', 'st-failed', 'st-waiting'] },
]

const TOKEN_LABEL: Record<ColorToken, string> = {
  'bg-0': 'App ground',
  'bg-1': 'Panels',
  'bg-2': 'Toolbars & cards',
  'bg-3': 'Popovers',
  inset: 'Canvas & wells',
  'line-1': 'Hairline dividers',
  'line-2': 'Interactive borders',
  'line-3': 'Strong borders',
  'fg-1': 'Primary text',
  'fg-2': 'Secondary text',
  'fg-3': 'Tertiary text',
  'fg-4': 'Faint text',
  accent: 'Accent',
  'accent-fg': 'Accent text',
  'st-idle': 'Pending',
  'st-running': 'Running',
  'st-done': 'Done',
  'st-failed': 'Failed',
  'st-waiting': 'Waiting',
}

function newSchemeId(): string {
  return `scheme-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/**
 * Custom colour schemes.
 *
 * Structural tokens (radius, motion, type) are never exposed here — only the
 * colour tokens in `COLOR_TOKENS` are, so a scheme can re-pick chrome tones or
 * the 5 status hues but can never make the design system's rules internally
 * inconsistent. `-weak` status variants are derived automatically at apply
 * time (color-mix), not edited directly.
 */
export function ColorSchemesSection({
  patch,
}: {
  patch: (next: SettingsPatch) => Promise<void>
}) {
  const colorSchemes = useStudio((s) => s.settings!.colorSchemes)
  const themePreference = useStudio((s) => s.settings!.appearance.theme)
  const resolvedBase = useResolvedTheme()
  const [editingId, setEditingId] = useState<string | null>(null)
  const [importError, setImportError] = useState<string | null>(null)

  const schemes = colorSchemes.schemes
  const editing = schemes.find((s) => s.id === editingId) ?? null

  async function writeSchemes(next: ColorScheme[], activeId = colorSchemes.activeId) {
    await patch({ colorSchemes: { schemes: next, activeId } })
  }

  async function createScheme() {
    const scheme: ColorScheme = {
      id: newSchemeId(),
      name: `Custom ${schemes.length + 1}`,
      base: resolvedBase,
      tokens: {},
    }
    await writeSchemes([...schemes, scheme])
    setEditingId(scheme.id)
  }

  async function createFromPreset(preset: (typeof PRESET_COLOR_SCHEMES)[number]) {
    const scheme: ColorScheme = { ...preset, id: newSchemeId() }
    const next = [...schemes, scheme]
    await patch({ appearance: { theme: 'custom' }, colorSchemes: { schemes: next, activeId: scheme.id } })
    setEditingId(scheme.id)
  }

  async function deleteScheme(id: string) {
    await writeSchemes(
      schemes.filter((s) => s.id !== id),
      colorSchemes.activeId === id ? null : colorSchemes.activeId,
    )
    if (editingId === id) setEditingId(null)
    if (colorSchemes.activeId === id && themePreference === 'custom') {
      await patch({ appearance: { theme: 'dark' } })
    }
  }

  async function applyScheme(id: string) {
    await patch({ appearance: { theme: 'custom' }, colorSchemes: { schemes, activeId: id } })
  }

  async function renameScheme(id: string, name: string) {
    await writeSchemes(schemes.map((s) => (s.id === id ? { ...s, name } : s)))
  }

  async function setToken(id: string, token: ColorToken, value: string | undefined) {
    const scheme = schemes.find((s) => s.id === id)
    if (!scheme) return
    const tokens = { ...scheme.tokens }
    if (value) tokens[token] = value
    else delete tokens[token]
    await writeSchemes(schemes.map((s) => (s.id === id ? { ...s, tokens } : s)))
  }

  async function exportScheme(scheme: ColorScheme) {
    await bridge?.theme.export(scheme)
  }

  async function importScheme() {
    if (!bridge) return
    const result = await bridge.theme.import()
    if (result.ok && result.scheme) {
      setImportError(null)
      const scheme: ColorScheme = { ...result.scheme, id: newSchemeId() }
      await writeSchemes([...schemes, scheme])
      setEditingId(scheme.id)
    } else if (result.error && result.error !== 'Import cancelled') {
      setImportError(result.error)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <span className="text-2xs text-fg-4">Start from a preset:</span>
        {PRESET_COLOR_SCHEMES.map((preset) => (
          <Button
            key={preset.name}
            size="sm"
            icon={<Sparkles size={12} strokeWidth={1.8} />}
            onClick={() => createFromPreset(preset)}
          >
            {preset.name}
          </Button>
        ))}
      </div>

      <div className="flex items-center justify-between">
        <div className="text-xs font-medium text-fg-2">Your schemes</div>
        <div className="flex gap-1.5">
          <Button size="sm" icon={<Upload size={12} strokeWidth={1.8} />} onClick={importScheme}>
            Import…
          </Button>
          <Button
            size="sm"
            variant="primary"
            icon={<Plus size={12} strokeWidth={1.8} />}
            onClick={createScheme}
          >
            New scheme
          </Button>
        </div>
      </div>

      {importError ? (
        <div className="pretty rounded-[var(--radius-control)] border border-st-failed/40 bg-st-failed-weak px-2.5 py-1.5 text-2xs text-st-failed">
          {importError}
        </div>
      ) : null}

      {schemes.length === 0 ? (
        <div className="pretty rounded-[var(--radius-card)] border border-dashed border-line-2 px-3 py-4 text-center text-xs text-fg-4">
          No custom schemes yet. Create one to override the status colours, accent and chrome
          tones.
        </div>
      ) : (
        <div className="flex flex-col gap-1.5">
          {schemes.map((scheme) => (
            <SchemeRow
              key={scheme.id}
              scheme={scheme}
              active={colorSchemes.activeId === scheme.id && themePreference === 'custom'}
              editing={editingId === scheme.id}
              onApply={() => applyScheme(scheme.id)}
              onToggleEdit={() => setEditingId(editingId === scheme.id ? null : scheme.id)}
              onDelete={() => deleteScheme(scheme.id)}
              onExport={() => exportScheme(scheme)}
              onRename={(name) => renameScheme(scheme.id, name)}
            />
          ))}
        </div>
      )}

      {editing ? (
        <div className="rounded-[var(--radius-card)] border border-line-2 bg-bg-1 p-3">
          {TOKEN_GROUPS.map((group) => (
            <div key={group.label} className="mb-3 last:mb-0">
              <div className="mb-1.5 text-2xs font-semibold tracking-[0.08em] text-fg-3 uppercase">
                {group.label}
              </div>
              <div className="grid grid-cols-2 gap-x-4 gap-y-1.5">
                {group.tokens.map((token) => (
                  <ColorRow
                    key={token}
                    label={TOKEN_LABEL[token]}
                    value={editing.tokens[token]}
                    defaultHex={BASE_HEX[editing.base][token]}
                    onChange={(value) => setToken(editing.id, token, value)}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function SchemeRow({
  scheme,
  active,
  editing,
  onApply,
  onToggleEdit,
  onDelete,
  onExport,
  onRename,
}: {
  scheme: ColorScheme
  active: boolean
  editing: boolean
  onApply: () => void
  onToggleEdit: () => void
  onDelete: () => void
  onExport: () => void
  onRename: (name: string) => void
}) {
  return (
    <div
      className={cn(
        'flex items-center gap-2 rounded-[var(--radius-control)] border px-2 py-1.5',
        active ? 'border-accent-line bg-accent-weak' : 'border-line-2',
      )}
    >
      <SwatchStrip tokens={scheme.tokens} base={scheme.base} />
      <Input
        value={scheme.name}
        onChange={(event) => onRename(event.target.value)}
        className="h-6 w-44 text-xs"
      />
      <span className="text-2xs text-fg-4 capitalize">{scheme.base}</span>
      <div className="ml-auto flex shrink-0 items-center gap-1">
        {active ? (
          <span className="px-1.5 text-2xs font-medium text-accent">Active</span>
        ) : (
          <Button size="sm" onClick={onApply}>
            Apply
          </Button>
        )}
        <IconButton label={editing ? 'Close editor' : 'Edit colours'} size="sm" active={editing} onClick={onToggleEdit}>
          <Palette size={13} strokeWidth={1.7} />
        </IconButton>
        <IconButton label="Export to file" size="sm" onClick={onExport}>
          <Download size={13} strokeWidth={1.7} />
        </IconButton>
        <IconButton label="Delete scheme" size="sm" onClick={onDelete}>
          <Trash2 size={13} strokeWidth={1.7} />
        </IconButton>
      </div>
    </div>
  )
}

function SwatchStrip({
  tokens,
  base,
}: {
  tokens: ColorScheme['tokens']
  base: ColorScheme['base']
}) {
  const preview: ColorToken[] = ['st-idle', 'st-running', 'st-done', 'st-failed', 'st-waiting']
  return (
    <div className="flex shrink-0 -space-x-1">
      {preview.map((token) => (
        <span
          key={token}
          className="h-3.5 w-3.5 rounded-full border border-bg-1"
          style={{ backgroundColor: tokens[token] ?? BASE_HEX[base][token] }}
        />
      ))}
    </div>
  )
}

function ColorRow({
  label,
  value,
  defaultHex,
  onChange,
}: {
  label: string
  value: string | undefined
  defaultHex: string
  onChange: (value: string | undefined) => void
}) {
  return (
    <label className="flex items-center gap-2 text-xs text-fg-2">
      <input
        type="color"
        value={/^#[0-9a-fA-F]{6}$/.test(value ?? '') ? value! : defaultHex}
        onChange={(event) => onChange(event.target.value)}
        className="h-6 w-6 shrink-0 cursor-pointer rounded-[3px] border border-line-2 bg-transparent p-0"
        title={label}
      />
      <span className="min-w-0 flex-1 truncate-1">{label}</span>
      {value ? (
        <button
          type="button"
          onClick={() => onChange(undefined)}
          className="text-2xs text-fg-4 hover:text-fg-2"
          title="Reset to the base palette's value"
        >
          reset
        </button>
      ) : null}
    </label>
  )
}
