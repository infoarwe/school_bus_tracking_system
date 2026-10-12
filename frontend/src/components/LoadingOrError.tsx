import type { ReactNode } from 'react'
import { Alert, Button, Skeleton } from 'antd'

/** Skeleton (or a custom placeholder) while loading; the error with a Retry button if loading failed. */
export default function LoadingOrError({
  error,
  onRetry,
  title = 'Could not load these settings',
  placeholder,
}: {
  error: string | null
  onRetry: () => void
  title?: string
  placeholder?: ReactNode
}) {
  if (!error) return <>{placeholder ?? <Skeleton active />}</>
  return (
    <Alert
      type="error"
      showIcon
      title={title}
      description={error}
      action={
        <Button size="small" onClick={onRetry}>
          Retry
        </Button>
      }
    />
  )
}
