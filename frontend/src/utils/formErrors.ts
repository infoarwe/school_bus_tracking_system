import type { FormInstance } from 'antd'
import { ApiError } from '../services/api'

/**
 * Shows API field errors next to the matching form fields. Returns the message to
 * show for anything that is not on a visible field (or null if every error was placed).
 */
export function applyApiErrors(
  form: FormInstance,
  err: unknown,
  /** API field name → form field name, when they differ. */
  rename: Record<string, string> = {},
): string | null {
  if (err instanceof ApiError && err.fields) {
    const onForm = new Set(Object.keys(form.getFieldsValue()))
    const placed: { name: string; errors: string[] }[] = []
    const elsewhere: string[] = []
    for (const [apiName, msg] of Object.entries(err.fields)) {
      const name = rename[apiName] ?? apiName
      if (onForm.has(name)) placed.push({ name, errors: [fieldMessage(msg)] })
      else elsewhere.push(`${fieldLabel(name)} ${msg}`)
    }
    form.setFields(placed)
    if (placed.length) {
      // Bring the first problem into view (long forms, small screens).
      form.scrollToField(placed[0].name, { block: 'center', behavior: 'smooth' })
    }
    return elsewhere.length ? capitalise(elsewhere.join('; ')) + '.' : null
  }
  return errorMessage(err)
}

/** A safe, human message for any error (never raw server output). */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'not_found':
        return 'This record was not found. It may have been removed, or it belongs to another school.'
      case 'forbidden':
        return 'Your role does not allow this action. Ask a School Admin if you need it.'
      default:
        return err.message || 'The request failed. Please try again.'
    }
  }
  return 'Something unexpected happened. Please reload the page and try again.'
}

const capitalise = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s)
const fieldLabel = (name: string) => capitalise(name.replaceAll('_id', '').replaceAll('_', ' '))

/** The server's short field reasons, phrased for a form. */
function fieldMessage(msg: string): string {
  if (msg === 'already exists') return 'Already used by another record. Enter a different value.'
  return capitalise(msg)
}
