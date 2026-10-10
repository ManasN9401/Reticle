import { X } from 'lucide-react'
import { Button, IconButton, StatusPip } from '@/design/primitives'
import { useToasts } from '@/state/toasts'

/** Stack of run notifications, anchored above the status bar. */
export function Toasts() {
  const toasts = useToasts((s) => s.toasts)
  const dismiss = useToasts((s) => s.dismiss)
  if (toasts.length === 0) return null

  return (
    <div
      aria-live="polite"
      className="pointer-events-none fixed right-4 bottom-9 z-50 flex w-[360px] max-w-[calc(100vw-2rem)] flex-col gap-2"
    >
      {toasts.map((toast) => (
        <div
          key={toast.id}
          role="status"
          className="pointer-events-auto rounded-[var(--radius-control)] border border-line-2 bg-bg-1 p-3 shadow-lg"
        >
          <div className="flex items-start gap-2">
            <span className="mt-1.5">
              <StatusPip
                color={toast.tone === 'success' ? 'var(--color-st-done)' : toast.tone === 'attention' ? 'var(--color-st-waiting)' : 'var(--color-st-failed)'}
                pulse={toast.tone === 'attention'}
              />
            </span>
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium text-fg-1">{toast.title}</p>
              {toast.detail ? (
                <p className="pretty mt-0.5 text-2xs break-words text-fg-3">{toast.detail}</p>
              ) : null}
            </div>
            <IconButton label="Dismiss" size="sm" onClick={() => dismiss(toast.id)}>
              <X size={12} />
            </IconButton>
          </div>
          {toast.actions.length > 0 ? (
            <div className="mt-2.5 flex flex-wrap justify-end gap-1.5">
              {toast.actions.map((action, index) => (
                <Button
                  key={action.label}
                  size="sm"
                  variant={index === 0 ? 'primary' : 'default'}
                  onClick={() => {
                    action.run()
                    dismiss(toast.id)
                  }}
                >
                  {action.label}
                </Button>
              ))}
            </div>
          ) : null}
        </div>
      ))}
    </div>
  )
}
