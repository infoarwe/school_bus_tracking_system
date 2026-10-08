import type { FormInstance } from 'antd'
import { ApiError } from '../services/api'

/**
 * Shows API field errors next to the matching form fields.
 * Returns the message to show for anything that is not field-specific.
 */
export function applyApiErrors(form: FormInstance, err: unknown): string | null {
  if (err instanceof ApiError && err.fields) {
    form.setFields(Object.entries(err.fields).map(([name, msg]) => ({ name, errors: [msg] })))
    return null
  }
  return err instanceof Error ? err.message : 'Something went wrong.'
}

export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'Something went wrong.'
}
