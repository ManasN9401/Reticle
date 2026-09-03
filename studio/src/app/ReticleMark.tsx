/**
 * The reticle: a crosshair with a broken ring.
 *
 * Used as the app mark and, at low opacity, as the graph canvas's centre
 * indicator. Drawn rather than imported so it inherits `currentColor` and stays
 * crisp at 14px.
 */
export function ReticleMark({ size = 14 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <circle cx="8" cy="8" r="5.25" stroke="currentColor" strokeWidth="1.25" opacity="0.55" />
      <circle cx="8" cy="8" r="1.5" fill="currentColor" />
      <path
        d="M8 0.75v3.1M8 12.15v3.1M0.75 8h3.1M12.15 8h3.1"
        stroke="currentColor"
        strokeWidth="1.25"
        strokeLinecap="round"
      />
    </svg>
  )
}
