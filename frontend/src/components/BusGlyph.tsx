/** A bus (as on the live map marker, see busMarker.ts) on a tinted square: bus lists and panels. */
export function BusGlyph({ color, size = 36 }: { color: string; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 36 36"
      aria-hidden
      style={{ flex: 'none', display: 'block' }}
    >
      <rect width="36" height="36" rx="10" fill={color} opacity="0.15" />
      <circle cx="18" cy="18" r="13" fill={color} />
      <g transform="translate(9 8)" fill="#fff">
        <rect x="-0.8" y="3" width="1.8" height="4.5" rx="0.9" />
        <rect x="17" y="3" width="1.8" height="4.5" rx="0.9" />
        <rect x="1" y="0" width="16" height="17" rx="3" />
        <rect x="3" y="2.5" width="12" height="7" rx="1.2" fill={color} />
        <circle cx="4.6" cy="13.4" r="1.4" fill={color} />
        <circle cx="13.4" cy="13.4" r="1.4" fill={color} />
        <rect x="2.5" y="16" width="3.6" height="3.6" rx="1" />
        <rect x="11.9" y="16" width="3.6" height="3.6" rx="1" />
      </g>
    </svg>
  )
}
