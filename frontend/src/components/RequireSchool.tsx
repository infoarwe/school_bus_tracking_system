import type { ReactNode } from 'react'
import { Empty } from 'antd'
import { useCurrentSchool } from '../context/SchoolContext'

/**
 * Renders children with the current school ID. A Super Admin who has not picked
 * a school yet sees a prompt instead. Keyed by school so state resets on switch.
 */
export default function RequireSchool({
  children,
}: {
  children: (schoolId: string, schoolName: string) => ReactNode
}) {
  const { schoolId, school } = useCurrentSchool()
  if (!schoolId) {
    return (
      <Empty style={{ marginTop: 80 }} description="Select a school in the header to continue." />
    )
  }
  return <div key={schoolId}>{children(schoolId, school?.name ?? '')}</div>
}
