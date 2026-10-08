import { Tag } from 'antd'
import { statusLabels } from '../utils/status'

const colors: Record<string, string> = {
  active: 'green',
  inactive: 'default',
  suspended: 'red',
  maintenance: 'orange',
}

export default function StatusTag({ status }: { status: string }) {
  return <Tag color={colors[status] ?? 'default'}>{statusLabels[status] ?? status}</Tag>
}
