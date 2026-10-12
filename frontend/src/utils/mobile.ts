/**
 * The 10-digit Indian mobile number in any common format ("98765 43210",
 * "+91 98765 43210", "098765 43210"), or null. Mirrors validate.NormalizeMobile.
 */
export function mobileDigits(s: string | undefined | null): string | null {
  let d = (s ?? '').replace(/\D/g, '')
  if (d.length === 12 && d.startsWith('91')) d = d.slice(2)
  else if (d.length === 11 && d.startsWith('0')) d = d.slice(1)
  return d.length === 10 && d[0] >= '6' ? d : null
}

/** "+919876543210" → "+91 98765 43210" for display. */
export function formatMobile(m: string | null | undefined): string {
  const d = mobileDigits(m)
  return d ? `+91 ${d.slice(0, 5)} ${d.slice(5)}` : (m ?? '')
}
