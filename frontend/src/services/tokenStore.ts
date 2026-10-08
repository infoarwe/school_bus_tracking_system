// Tokens live in localStorage so a page reload keeps the user logged in.
// Storage can be unavailable (private mode, blocked site data), so every access is guarded.

const ACCESS = 'sbts.access_token'
const REFRESH = 'sbts.refresh_token'

function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function write(key: string, value: string | null) {
  try {
    if (value === null) localStorage.removeItem(key)
    else localStorage.setItem(key, value)
  } catch {
    // ignore: the session just won't survive a reload
  }
}

let access: string | null = read(ACCESS)
let refresh: string | null = read(REFRESH)

export const tokenStore = {
  get access() {
    return access
  },
  get refresh() {
    return refresh
  },
  set(tokens: { access_token: string; refresh_token: string }) {
    access = tokens.access_token
    refresh = tokens.refresh_token
    write(ACCESS, access)
    write(REFRESH, refresh)
  },
  clear() {
    access = null
    refresh = null
    write(ACCESS, null)
    write(REFRESH, null)
  },
}
