/** Front view of a bus in an 18×20 box, drawn in white on `color` (BusGlyph draws the same). */
function busGlyph(color: string) {
  return (
    `<rect x="-0.8" y="3" width="1.8" height="4.5" rx="0.9" fill="#fff"/>` +
    `<rect x="17" y="3" width="1.8" height="4.5" rx="0.9" fill="#fff"/>` +
    `<rect x="1" y="0" width="16" height="17" rx="3" fill="#fff"/>` +
    `<rect x="3" y="2.5" width="12" height="7" rx="1.2" fill="${color}"/>` +
    `<circle cx="4.6" cy="13.4" r="1.4" fill="${color}"/>` +
    `<circle cx="13.4" cy="13.4" r="1.4" fill="${color}"/>` +
    `<rect x="2.5" y="16" width="3.6" height="3.6" rx="1" fill="#fff"/>` +
    `<rect x="11.9" y="16" width="3.6" height="3.6" rx="1" fill="#fff"/>`
  )
}

const escapeXml = (s: string) =>
  s.replace(
    /[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!,
  )

const R = 17 // bus circle radius
const CY = 28 // circle centre; the heading pointer reaches 27 px out
const PILL_Y = 58

/**
 * Map marker: a round bus badge in the state colour, a pointer showing the
 * direction of travel (when known), and the bus number on a label underneath.
 * The marker's anchor is the centre of the badge.
 */
export function busMarkerIcon(opts: {
  label: string
  color: string
  heading: number | null
  selected: boolean
}): google.maps.Icon {
  const label = escapeXml(opts.label)
  const w = Math.max(60, Math.ceil(opts.label.length * 7.2) + 20)
  const cx = w / 2
  const ring = opts.selected ? '#1d4ed8' : '#ffffff'
  const pill = opts.selected ? '#1d4ed8' : '#1f2937'
  const pointer =
    opts.heading == null
      ? ''
      : `<path d="M${cx} ${CY - 27} L${cx + 8} ${CY - 14} L${cx - 8} ${CY - 14} Z" fill="${opts.color}" ` +
        `stroke="#fff" stroke-width="1.5" stroke-linejoin="round" transform="rotate(${Math.round(opts.heading)} ${cx} ${CY})"/>`
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="80" viewBox="0 0 ${w} 80">` +
    `<defs><filter id="s" x="-50%" y="-50%" width="200%" height="200%">` +
    `<feDropShadow dx="0" dy="1" stdDeviation="1.5" flood-opacity="0.35"/></filter></defs>` +
    pointer +
    `<circle cx="${cx}" cy="${CY}" r="${R}" fill="${opts.color}" stroke="${ring}" stroke-width="3" filter="url(#s)"/>` +
    `<g transform="translate(${cx - 9} ${CY - 10})">${busGlyph(opts.color)}</g>` +
    `<rect x="1" y="${PILL_Y}" width="${w - 2}" height="20" rx="10" fill="${pill}" stroke="#fff" stroke-width="1.5" filter="url(#s)"/>` +
    `<text x="${cx}" y="${PILL_Y + 14}" text-anchor="middle" font-family="Segoe UI, Roboto, Arial, sans-serif" ` +
    `font-size="11" font-weight="700" fill="#fff">${label}</text>` +
    `</svg>`
  return {
    url: `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`,
    scaledSize: new google.maps.Size(w, 80),
    anchor: new google.maps.Point(cx, CY),
  }
}
