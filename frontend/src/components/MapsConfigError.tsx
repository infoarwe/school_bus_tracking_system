import { useNavigate } from 'react-router-dom'
import { Alert, Button, Flex } from 'antd'
import { EnvironmentOutlined } from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'

/**
 * Shown wherever a map or place search would otherwise render, when the
 * current school has no Google Maps browser key configured. Google Maps is
 * the only map provider in this app: there is no OpenStreetMap/Leaflet
 * fallback, so this is a clear, actionable error instead of a silently
 * degraded map.
 */
export default function MapsConfigError({
  height,
  compact,
}: {
  /** Fills a map-sized area; omit for an inline (form-field-sized) message. */
  height?: number
  /** A shorter message for tight spaces (e.g. inside a search field). */
  compact?: boolean
}) {
  const navigate = useNavigate()
  const { hasRole } = useAuth()
  const canConfigure = hasRole('super_admin', 'school_admin')

  const body = canConfigure
    ? 'Add a Google Maps browser key in Settings → Maps to enable maps and place search for this school.'
    : 'Ask a School Admin to add a Google Maps key in Settings → Maps to enable maps and place search for this school.'

  const alert = (
    <Alert
      type="warning"
      showIcon
      icon={<EnvironmentOutlined />}
      title={compact ? body : 'Google Maps is not configured for this school'}
      description={compact ? undefined : body}
      action={
        canConfigure && !compact ? (
          <Button size="small" onClick={() => navigate('/settings')}>
            Open Settings
          </Button>
        ) : undefined
      }
    />
  )

  if (height == null) return alert

  return (
    <Flex
      vertical
      align="center"
      justify="center"
      gap="middle"
      style={{
        height,
        width: '100%',
        borderRadius: 8,
        background: '#fafafa',
        border: '1px dashed #d9d9d9',
        padding: 24,
      }}
    >
      <div style={{ maxWidth: 420 }}>{alert}</div>
      {canConfigure && (
        <Button icon={<EnvironmentOutlined />} onClick={() => navigate('/settings')}>
          Open Settings → Maps
        </Button>
      )}
    </Flex>
  )
}
