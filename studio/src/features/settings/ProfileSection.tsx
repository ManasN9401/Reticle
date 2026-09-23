import { useState } from 'react'
import { Download, Plus, Trash2, Upload } from 'lucide-react'
import { cn } from '@/design/cn'
import { Button, IconButton, Input } from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import type { SettingsPatch, SettingsProfile } from '@shared/ipc'

function newProfileId(): string {
  return `profile-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/**
 * Settings profiles — a VS Code-style bundle of {Appearance, Node Appearance,
 * Colour Schemes, Connection}, saved under a name and switchable. Forge, API
 * Keys, Models and Logs stay outside a profile — they're more
 * project/secret-specific than "look and feel."
 *
 * Applying is a one-shot patch, not a live binding: editing settings
 * afterward doesn't retroactively change the profile it came from.
 */
export function ProfileSection({
  patch,
}: {
  patch: (next: SettingsPatch) => Promise<void>
}) {
  const profiles = useStudio((s) => s.settings!.profiles)
  const settings = useStudio((s) => s.settings!)
  const [importError, setImportError] = useState<string | null>(null)

  async function createFromCurrent() {
    const profile: SettingsProfile = {
      id: newProfileId(),
      name: `Profile ${profiles.profiles.length + 1}`,
      snapshot: {
        appearance: settings.appearance,
        nodeAppearance: settings.nodeAppearance,
        colorSchemes: settings.colorSchemes,
        connection: settings.connection,
      },
    }
    await patch({
      profiles: { profiles: [...profiles.profiles, profile], activeId: profiles.activeId },
    })
  }

  async function applyProfile(profile: SettingsProfile) {
    await patch({
      appearance: profile.snapshot.appearance,
      nodeAppearance: profile.snapshot.nodeAppearance,
      colorSchemes: profile.snapshot.colorSchemes,
      connection: profile.snapshot.connection,
      profiles: { profiles: profiles.profiles, activeId: profile.id },
    })
  }

  async function renameProfile(id: string, name: string) {
    await patch({
      profiles: {
        profiles: profiles.profiles.map((p) => (p.id === id ? { ...p, name } : p)),
        activeId: profiles.activeId,
      },
    })
  }

  async function deleteProfile(id: string) {
    await patch({
      profiles: {
        profiles: profiles.profiles.filter((p) => p.id !== id),
        activeId: profiles.activeId === id ? null : profiles.activeId,
      },
    })
  }

  async function exportProfile(profile: SettingsProfile) {
    await bridge?.profile.export(profile)
  }

  async function importProfile() {
    if (!bridge) return
    const result = await bridge.profile.import()
    if (result.ok && result.profile) {
      setImportError(null)
      const profile: SettingsProfile = { ...result.profile, id: newProfileId() }
      await patch({
        profiles: { profiles: [...profiles.profiles, profile], activeId: profiles.activeId },
      })
    } else if (result.error && result.error !== 'Import cancelled') {
      setImportError(result.error)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div className="text-xs font-medium text-fg-2">Your profiles</div>
        <div className="flex gap-1.5">
          <Button size="sm" icon={<Upload size={12} strokeWidth={1.8} />} onClick={importProfile}>
            Import…
          </Button>
          <Button
            size="sm"
            variant="primary"
            icon={<Plus size={12} strokeWidth={1.8} />}
            onClick={createFromCurrent}
          >
            New from current
          </Button>
        </div>
      </div>

      {importError ? (
        <div className="pretty rounded-[var(--radius-control)] border border-st-failed/40 bg-st-failed-weak px-2.5 py-1.5 text-2xs text-st-failed">
          {importError}
        </div>
      ) : null}

      {profiles.profiles.length === 0 ? (
        <div className="pretty rounded-[var(--radius-card)] border border-dashed border-line-2 px-3 py-4 text-center text-xs text-fg-4">
          No profiles yet. "New from current" saves your Appearance, Node Appearance, Colour
          Scheme and Connection settings as a named, switchable bundle.
        </div>
      ) : (
        <div className="flex flex-col gap-1.5">
          {profiles.profiles.map((profile) => (
            <div
              key={profile.id}
              className={cn(
                'flex items-center gap-2 rounded-[var(--radius-control)] border px-2 py-1.5',
                profiles.activeId === profile.id
                  ? 'border-accent-line bg-accent-weak'
                  : 'border-line-2',
              )}
            >
              <Input
                value={profile.name}
                onChange={(event) => renameProfile(profile.id, event.target.value)}
                className="h-6 w-48 text-xs"
              />
              <div className="ml-auto flex shrink-0 items-center gap-1">
                {profiles.activeId === profile.id ? (
                  <span className="px-1.5 text-2xs font-medium text-accent">Active</span>
                ) : (
                  <Button size="sm" onClick={() => applyProfile(profile)}>
                    Apply
                  </Button>
                )}
                <IconButton
                  label="Export to file"
                  size="sm"
                  onClick={() => exportProfile(profile)}
                >
                  <Download size={13} strokeWidth={1.7} />
                </IconButton>
                <IconButton
                  label="Delete profile"
                  size="sm"
                  onClick={() => deleteProfile(profile.id)}
                >
                  <Trash2 size={13} strokeWidth={1.7} />
                </IconButton>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
