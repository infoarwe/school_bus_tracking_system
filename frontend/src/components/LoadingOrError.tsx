import { Alert, Button, Skeleton } from 'antd'

/** Skeleton while loading; the error with a Retry button if loading failed. */
export default function LoadingOrError({
  error,
  onRetry,
}: {
  error: string | null
  onRetry: () => void
}) {
  if (!error) return <Skeleton active />
  return (
    <Alert
      type="error"
      showIcon
      title="Could not load these settings"
      description={`${error} If the API was just updated, restart it and try again.`}
      action={
        <Button size="small" onClick={onRetry}>
          Retry
        </Button>
      }
    />
  )
}
