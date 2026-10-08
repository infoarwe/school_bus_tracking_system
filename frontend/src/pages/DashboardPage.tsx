import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Descriptions, Space, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { fetchHealth, type Health } from '../services/health'

// Sprint 0 placeholder: proves the web app can reach the Go API.
// The real dashboard (totals, live buses, alerts) is built in Sprint 8.
export default function DashboardPage() {
  const [health, setHealth] = useState<Health | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    fetchHealth()
      .then((h) => {
        if (cancelled) return
        setHealth(h)
        setError(null)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setHealth(null)
        setError(e instanceof Error ? e.message : 'Health check failed')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reloadKey])

  const reload = useCallback(() => {
    setLoading(true)
    setReloadKey((k) => k + 1)
  }, [])

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Typography.Title level={3}>Dashboard</Typography.Title>
      <Card
        title="Backend status"
        extra={
          <Button icon={<ReloadOutlined />} loading={loading} onClick={reload}>
            Refresh
          </Button>
        }
      >
        {error && <Alert type="error" showIcon message={error} />}
        {health && (
          <Descriptions column={1} bordered size="small">
            <Descriptions.Item label="API">
              <Tag color={health.status === 'ok' ? 'green' : 'orange'}>{health.status}</Tag>
            </Descriptions.Item>
            {Object.entries(health.checks).map(([name, state]) => (
              <Descriptions.Item key={name} label={name}>
                <Tag color={state === 'up' ? 'green' : 'red'}>{state}</Tag>
              </Descriptions.Item>
            ))}
          </Descriptions>
        )}
      </Card>
    </Space>
  )
}
