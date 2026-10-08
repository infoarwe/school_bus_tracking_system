import type { ReactNode } from 'react'
import { Flex, Input, Select } from 'antd'
import { statusLabels } from '../utils/status'

/** Search box + status filter used above every master-data table. */
export default function ListToolbar({
  placeholder,
  statuses,
  status,
  onSearch,
  onStatus,
  children,
}: {
  placeholder: string
  statuses: string[]
  status?: string
  onSearch: (q: string) => void
  onStatus: (status: string) => void
  children?: ReactNode
}) {
  return (
    <Flex gap="small" wrap>
      <Input.Search
        allowClear
        placeholder={placeholder}
        style={{ width: 260 }}
        onSearch={onSearch}
      />
      <Select
        style={{ width: 160 }}
        value={status ?? ''}
        onChange={onStatus}
        options={statusOptions(statuses)}
      />
      {children}
    </Flex>
  )
}

function statusOptions(statuses: string[]) {
  return [
    { value: '', label: 'All statuses' },
    ...statuses.map((s) => ({ value: s, label: statusLabels[s] ?? s })),
  ]
}
