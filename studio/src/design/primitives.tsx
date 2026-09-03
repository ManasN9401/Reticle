import {
  forwardRef,
  useId,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
} from 'react'
import { cn } from './cn'

/**
 * Shared control primitives.
 *
 * Two rules run through all of them:
 *  - transitions name the properties they animate (never `all`), so a hover
 *    never accidentally animates layout;
 *  - anything interactive keeps at least a 32px hit target even when the glyph
 *    inside is 14px.
 */

const FOCUS =
  'focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-1'

const TRANSITION =
  '[transition-property:background-color,border-color,color,box-shadow,transform,opacity] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]'

// ---------------------------------------------------------------------------
// Button
// ---------------------------------------------------------------------------

type ButtonVariant = 'primary' | 'default' | 'subtle' | 'danger'
type ButtonSize = 'sm' | 'md'

const BUTTON_VARIANTS: Record<ButtonVariant, string> = {
  primary:
    'bg-accent text-accent-fg border-transparent hover:brightness-110 active:brightness-95',
  default:
    'bg-bg-2 text-fg-1 border-line-2 hover:bg-bg-3 hover:border-line-3 active:bg-bg-2',
  subtle:
    'bg-transparent text-fg-2 border-transparent hover:bg-bg-2 hover:text-fg-1 active:bg-bg-3',
  danger:
    'bg-transparent text-st-failed border-st-failed/40 hover:bg-st-failed-weak active:brightness-95',
}

const BUTTON_SIZES: Record<ButtonSize, string> = {
  sm: 'h-6 px-2 text-xs gap-1.5',
  md: 'h-8 px-3 text-sm gap-2',
}

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
  icon?: ReactNode
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'default', size = 'md', icon, className, children, ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type="button"
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-[var(--radius-control)] border font-medium whitespace-nowrap select-none',
        'disabled:pointer-events-none disabled:opacity-40',
        'active:scale-[0.97]',
        BUTTON_VARIANTS[variant],
        BUTTON_SIZES[size],
        TRANSITION,
        FOCUS,
        className,
      )}
      {...rest}
    >
      {icon}
      {children}
    </button>
  )
})

// ---------------------------------------------------------------------------
// IconButton
// ---------------------------------------------------------------------------

export interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  label: string
  active?: boolean
  size?: 'sm' | 'md'
}

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(
  function IconButton({ label, active, size = 'md', className, children, ...rest }, ref) {
    return (
      <button
        ref={ref}
        type="button"
        title={label}
        aria-label={label}
        aria-pressed={active}
        className={cn(
          'inline-flex shrink-0 items-center justify-center rounded-[var(--radius-control)]',
          size === 'sm' ? 'h-6 w-6' : 'h-8 w-8',
          active ? 'bg-bg-3 text-fg-1' : 'text-fg-3 hover:bg-bg-2 hover:text-fg-1',
          'disabled:pointer-events-none disabled:opacity-35',
          'active:scale-[0.94]',
          TRANSITION,
          FOCUS,
          className,
        )}
        {...rest}
      >
        {children}
      </button>
    )
  },
)

// ---------------------------------------------------------------------------
// StatusPip
// ---------------------------------------------------------------------------

export interface StatusPipProps {
  color: string
  /** Pulse to signal live activity — suppressed under reduced motion. */
  pulse?: boolean
  size?: number
  className?: string
}

export function StatusPip({ color, pulse, size = 7, className }: StatusPipProps) {
  return (
    <span
      className={cn('relative inline-block shrink-0 rounded-full', className)}
      style={{ width: size, height: size, backgroundColor: color }}
    >
      {pulse ? (
        <span
          className="absolute inset-0 animate-ping rounded-full opacity-60"
          style={{ backgroundColor: color }}
        />
      ) : null}
    </span>
  )
}

// ---------------------------------------------------------------------------
// Badge / Chip
// ---------------------------------------------------------------------------

export function Badge({
  children,
  color,
  className,
}: {
  children: ReactNode
  color?: string
  className?: string
}) {
  return (
    <span
      className={cn(
        'inline-flex h-[18px] items-center gap-1 rounded-[3px] px-1.5 text-2xs font-medium tracking-wide uppercase',
        color ? '' : 'bg-bg-3 text-fg-2',
        className,
      )}
      style={color ? { color, backgroundColor: 'color-mix(in srgb, currentColor 14%, transparent)' } : undefined}
    >
      {children}
    </span>
  )
}

export function Chip({
  children,
  title,
  className,
}: {
  children: ReactNode
  title?: string
  className?: string
}) {
  return (
    <span
      title={title}
      className={cn(
        'mono inline-flex h-[17px] max-w-full items-center rounded-[3px] border border-line-2 bg-bg-2 px-1 text-2xs text-fg-3',
        'truncate-1',
        className,
      )}
    >
      {children}
    </span>
  )
}

// ---------------------------------------------------------------------------
// Inputs
// ---------------------------------------------------------------------------

const FIELD_BASE =
  'w-full rounded-[var(--radius-control)] border border-line-2 bg-inset px-2 text-sm text-fg-1 placeholder:text-fg-4 disabled:opacity-40'

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  function Input({ className, ...rest }, ref) {
    return (
      <input
        ref={ref}
        className={cn(FIELD_BASE, 'h-8', TRANSITION, FOCUS, 'hover:border-line-3', className)}
        {...rest}
      />
    )
  },
)

export const Select = forwardRef<
  HTMLSelectElement,
  SelectHTMLAttributes<HTMLSelectElement>
>(function Select({ className, children, ...rest }, ref) {
  return (
    <select
      ref={ref}
      className={cn(
        FIELD_BASE,
        'h-8 cursor-pointer appearance-none pr-6',
        TRANSITION,
        FOCUS,
        'hover:border-line-3',
        className,
      )}
      {...rest}
    >
      {children}
    </select>
  )
})

export function Field({
  label,
  hint,
  children,
  className,
}: {
  label: string
  hint?: string
  children: ReactNode
  className?: string
}) {
  const id = useId()
  return (
    <label className={cn('flex flex-col gap-1', className)} htmlFor={id}>
      <span className="text-xs font-medium text-fg-2">{label}</span>
      {children}
      {hint ? <span className="pretty text-2xs text-fg-4">{hint}</span> : null}
    </label>
  )
}

export function Toggle({
  checked,
  onChange,
  label,
  disabled,
}: {
  checked: boolean
  onChange: (next: boolean) => void
  label: string
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative inline-flex h-[18px] w-8 shrink-0 items-center rounded-full border',
        checked ? 'border-transparent bg-accent' : 'border-line-2 bg-bg-3',
        'disabled:pointer-events-none disabled:opacity-40',
        TRANSITION,
        FOCUS,
      )}
    >
      <span
        className={cn(
          'block h-3 w-3 rounded-full bg-fg-1 [transition-property:transform] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]',
          checked ? 'translate-x-[17px]' : 'translate-x-[3px]',
        )}
      />
    </button>
  )
}

// ---------------------------------------------------------------------------
// Tooltip — hover/focus only, no portal. Enough for dense chrome.
// ---------------------------------------------------------------------------

export function Tooltip({
  content,
  children,
  side = 'bottom',
}: {
  content: ReactNode
  children: ReactNode
  side?: 'top' | 'bottom'
}) {
  const [open, setOpen] = useState(false)
  return (
    <span
      className="relative inline-flex"
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
      onFocus={() => setOpen(true)}
      onBlur={() => setOpen(false)}
    >
      {children}
      {open ? (
        <span
          role="tooltip"
          className={cn(
            'pointer-events-none absolute left-1/2 z-50 -translate-x-1/2 rounded-[var(--radius-control)] border border-line-2 bg-bg-3 px-2 py-1 text-xs whitespace-nowrap text-fg-1 shadow-[var(--shadow-pop)]',
            side === 'bottom' ? 'top-[calc(100%+6px)]' : 'bottom-[calc(100%+6px)]',
          )}
        >
          {content}
        </span>
      ) : null}
    </span>
  )
}

// ---------------------------------------------------------------------------
// Kbd / EmptyState / SectionLabel
// ---------------------------------------------------------------------------

export function Kbd({ children }: { children: ReactNode }) {
  return (
    <kbd className="mono inline-flex h-[17px] items-center rounded-[3px] border border-line-2 bg-bg-2 px-1 text-2xs text-fg-3">
      {children}
    </kbd>
  )
}

export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: {
  icon?: ReactNode
  title: string
  description?: ReactNode
  action?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex h-full flex-col items-center justify-center gap-2 px-6 py-8 text-center',
        className,
      )}
    >
      {icon ? <div className="text-fg-4">{icon}</div> : null}
      <div className="balance text-sm font-medium text-fg-2">{title}</div>
      {description ? (
        <div className="pretty max-w-[46ch] text-xs leading-relaxed text-fg-4">
          {description}
        </div>
      ) : null}
      {action ? <div className="mt-1">{action}</div> : null}
    </div>
  )
}

export function SectionLabel({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex h-6 items-center px-3 text-2xs font-semibold tracking-[0.08em] text-fg-3 uppercase',
        className,
      )}
    >
      {children}
    </div>
  )
}

export function Divider({ className }: { className?: string }) {
  return <div className={cn('h-px shrink-0 bg-line-1', className)} />
}

export function Spinner({ size = 12 }: { size?: number }) {
  return (
    <span
      className="inline-block shrink-0 animate-spin rounded-full border-[1.5px] border-line-3 border-t-fg-2"
      style={{ width: size, height: size }}
    />
  )
}
