import { useNavigate } from 'react-router-dom'
import { Card, Empty, Flex, Tag, Typography } from 'antd'
import type { DashboardData, TripStatus } from '../../services/types'

type OutingRow = DashboardData['outings'][number]

const statusBadge: Record<TripStatus, { color: string; label: string }> = {
  scheduled: { color: 'default', label: 'Scheduled' },
  confirmed: { color: 'cyan', label: 'Confirmed' },
  started: { color: 'green', label: 'In progress' },
  completed: { color: 'blue', label: 'Completed' },
  cancelled: { color: 'red', label: 'Cancelled' },
}

/**
 * Today's Special Trips/Outings — distinguished from the route-based list.
 * The backend always sends `outings` as an array (never omitted or null), but
 * this component defends against it being missing anyway (e.g. an older API
 * process still running during a rollout) rather than crashing the dashboard.
 */
export default function OutingsPanel({ outings }: { outings: OutingRow[] | undefined }) {
  const navigate = useNavigate()
  const rows = outings ?? []
  return (
    <Card
      className="dash-panel"
      title={
        <Flex align="center" gap={8}>
          Special Trips / Outings today
          <Tag color="purple">{rows.length}</Tag>
        </Flex>
      }
    >
      {rows.length === 0 ? (
        <Empty
          description="No special trips today."
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          style={{ margin: '12px 0' }}
        />
      ) : (
        <Flex vertical gap={8}>
          {rows.map((o) => {
            const badge = statusBadge[o.status]
            return (
              <Flex
                key={o.trip_id}
                justify="space-between"
                align="center"
                className="quick-action"
                style={{ cursor: 'pointer' }}
                onClick={() => navigate(`/trips?date=${o.trip_date}`)}
              >
                <Flex vertical gap={0}>
                  <Typography.Text strong>{o.title || o.route_code}</Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {o.bus_number} · {o.driver_name}
                    {o.route_code && ` · based on ${o.route_code}`}
                  </Typography.Text>
                </Flex>
                <Tag color={badge.color}>{badge.label}</Tag>
              </Flex>
            )
          })}
        </Flex>
      )}
    </Card>
  )
}
