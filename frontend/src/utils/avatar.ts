const avatarColors = ['#1d4ed8', '#0891b2', '#7c3aed', '#c2410c', '#047857', '#be185d']

/** A stable colour per ID, so the same person looks the same on every page. */
export function avatarColor(id: string) {
  let h = 0
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return avatarColors[h % avatarColors.length]
}

/** Up to two initials from a name ("Kumar (Demo Driver)" → "KD"). */
export function initials(name: string) {
  const words = name
    .replace(/[^\p{L}\s]/gu, ' ')
    .trim()
    .split(/\s+/)
    .filter(Boolean)
  return words
    .slice(0, 2)
    .map((w) => w[0].toUpperCase())
    .join('')
}
