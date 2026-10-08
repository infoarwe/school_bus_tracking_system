import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { schoolsApi } from '../services/schools'
import type { School } from '../services/types'
import { useAuth } from './AuthContext'

// The school the admin pages work on. Staff are fixed to their own school;
// a Super Admin picks one from the header.

interface SchoolValue {
  schoolId: string | null
  school: School | null
  /** All schools; loaded for Super Admin only. */
  schools: School[]
  setSchoolId: (id: string | null) => void
  reloadSchools: () => Promise<void>
}

const SchoolContext = createContext<SchoolValue | null>(null)
const STORAGE_KEY = 'sbts.super_admin_school'

function readSaved(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

export function SchoolProvider({ children }: { children: ReactNode }) {
  const { me } = useAuth()
  const isSuper = me?.user.role === 'super_admin'
  const [schools, setSchools] = useState<School[]>([])
  const [selected, setSelected] = useState<string | null>(readSaved)

  const reloadSchools = useCallback(async () => {
    const res = await schoolsApi.list({ page_size: 100 })
    setSchools(res.data)
  }, [])

  useEffect(() => {
    if (!isSuper || me?.two_factor_setup_required) return
    schoolsApi
      .list({ page_size: 100 })
      .then((res) => setSchools(res.data))
      .catch(() => setSchools([]))
  }, [isSuper, me?.two_factor_setup_required])

  const setSchoolId = useCallback((id: string | null) => {
    setSelected(id)
    try {
      if (id) localStorage.setItem(STORAGE_KEY, id)
      else localStorage.removeItem(STORAGE_KEY)
    } catch {
      // per-browser convenience only
    }
  }, [])

  const value = useMemo<SchoolValue>(() => {
    if (!isSuper) {
      return {
        schoolId: me?.school?.id ?? null,
        school: me?.school ?? null,
        schools: [],
        setSchoolId,
        reloadSchools,
      }
    }
    const school = schools.find((s) => s.id === selected) ?? null
    return { schoolId: school?.id ?? null, school, schools, setSchoolId, reloadSchools }
  }, [isSuper, me, schools, selected, setSchoolId, reloadSchools])

  return <SchoolContext.Provider value={value}>{children}</SchoolContext.Provider>
}

export function useCurrentSchool(): SchoolValue {
  const ctx = useContext(SchoolContext)
  if (!ctx) throw new Error('useCurrentSchool must be used inside SchoolProvider')
  return ctx
}
