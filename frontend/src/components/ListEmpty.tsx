import type { ReactNode } from 'react'
import { Button, Empty, Result } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'

/**
 * What a list table shows when it has no rows: the load error with Retry, "nothing
 * matches" when a search/filter is applied, or a first-use hint (with an action).
 */
export default function ListEmpty({
  list,
  empty,
  action,
}: {
  list: { error: string | null; filtered: boolean; reload: () => void }
  /** First-use text, e.g. "No buses yet." */
  empty: string
  action?: ReactNode
}) {
  if (list.error) {
    return (
      <Result
        status="warning"
        title="Could not load the list"
        subTitle={list.error}
        extra={
          <Button icon={<ReloadOutlined />} onClick={list.reload}>
            Try again
          </Button>
        }
      />
    )
  }
  if (list.filtered) {
    return <Empty description="Nothing matches your search or filters." />
  }
  return <Empty description={empty}>{action}</Empty>
}
