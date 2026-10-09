import { App, Button, Empty, Flex, Space, Tag, Typography } from 'antd'
import { useAlerts } from '../context/AlertsContext'
import { alertsApi } from '../services/alerts'
import { delayReasonLabels, tripTypeLabels, type Alert } from '../services/types'
import { errorMessage } from '../utils/formErrors'

const time = (iso: string) =>
  new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })

/** Emergencies (with acknowledge / resolve) and delays, as cards. */
export default function AlertList({ schoolId, compact }: { schoolId: string; compact?: boolean }) {
  const { message } = App.useApp()
  const { alerts, reload } = useAlerts()

  async function act(a: Alert, action: 'acknowledge' | 'resolve') {
    try {
      await alertsApi[action](schoolId, a.id)
      reload()
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  const shown = compact
    ? alerts.filter((a) => a.kind === 'delay' || a.status !== 'resolved').slice(0, 5)
    : alerts
  if (shown.length === 0) return <Empty description="No alerts today." />

  return (
    <Flex vertical gap="small">
      {shown.map((a) => (
        <div
          key={a.id}
          className="stop-row"
          style={{
            cursor: 'default',
            alignItems: 'flex-start',
            borderColor: a.kind === 'emergency' && a.status === 'open' ? '#ff4d4f' : undefined,
          }}
        >
          <div className="stop-row__body">
            <Space size={4} wrap>
              {a.kind === 'emergency' ? (
                <Tag
                  color={
                    a.status === 'resolved'
                      ? 'default'
                      : a.status === 'acknowledged'
                        ? 'orange'
                        : 'red'
                  }
                >
                  Emergency · {a.status}
                </Tag>
              ) : (
                <Tag color={a.reason === 'bus_breakdown' ? 'volcano' : 'gold'}>
                  {a.reason === 'bus_breakdown' ? 'Breakdown' : `Delay ${a.minutes} min`}
                </Tag>
              )}
              <Typography.Text strong>
                {a.route_code} · {a.bus_number}
              </Typography.Text>
            </Space>
            <Typography.Text>
              {a.kind === 'emergency'
                ? a.message
                : `${delayReasonLabels[a.reason!]}${a.note ? `: ${a.note}` : ''}`}
            </Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {time(a.created_at)} · {tripTypeLabels[a.trip_type]} · driver {a.driver_name}
              {a.reported_by &&
                a.reported_by !== a.driver_name &&
                ` · reported by ${a.reported_by}`}
              {a.kind === 'delay' && ' · parents notified'}
            </Typography.Text>
          </div>
          {a.kind === 'emergency' && a.status !== 'resolved' && (
            <Space size="small">
              {a.status === 'open' && (
                <Button size="small" onClick={() => void act(a, 'acknowledge')}>
                  Acknowledge
                </Button>
              )}
              <Button size="small" type="primary" onClick={() => void act(a, 'resolve')}>
                Resolve
              </Button>
            </Space>
          )}
        </div>
      ))}
    </Flex>
  )
}
