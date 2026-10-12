import type { ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Card, Typography } from 'antd'
import {
  ArrowRightOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  ToolOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { useAlerts } from '../../context/AlertsContext'
import { delayReasonLabels, tripTypeLabels, type Alert } from '../../services/types'
import { clockTime } from './time'

// tone: a colour name from global.css (.pill--*, .icon-tile--*).
type Severity = { rank: number; tone: string; label: string; icon: ReactNode }

/** Open emergencies first, then breakdowns, then delays; resolved emergencies last. */
function severity(a: Alert): Severity {
  if (a.kind === 'emergency') {
    if (a.status === 'open') {
      return { rank: 0, tone: 'red', label: 'Emergency', icon: <WarningOutlined /> }
    }
    if (a.status === 'acknowledged') {
      return {
        rank: 1,
        tone: 'orange',
        label: 'Emergency · acknowledged',
        icon: <WarningOutlined />,
      }
    }
    return { rank: 4, tone: 'slate', label: 'Emergency · resolved', icon: <WarningOutlined /> }
  }
  if (a.reason === 'bus_breakdown') {
    return { rank: 2, tone: 'red', label: 'Breakdown', icon: <ToolOutlined /> }
  }
  return {
    rank: 3,
    tone: 'orange',
    label: `Delay · ${a.minutes} min`,
    icon: <ClockCircleOutlined />,
  }
}

const MAX = 5

export default function AlertsPanel() {
  const navigate = useNavigate()
  const { alerts, openEmergencies } = useAlerts()
  const sorted = [...alerts].sort(
    (a, b) =>
      severity(a).rank - severity(b).rank ||
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  )

  return (
    <Card
      className="dash-panel"
      title={
        <span>
          Alerts today{' '}
          {openEmergencies.length > 0 && (
            <span className="pill pill--red" style={{ marginLeft: 6 }}>
              {openEmergencies.length} open
            </span>
          )}
        </span>
      }
      extra={
        <Button type="link" onClick={() => navigate('/alerts')} style={{ paddingInline: 0 }}>
          View all <ArrowRightOutlined />
        </Button>
      }
    >
      {sorted.length === 0 ? (
        <div className="dash-empty" style={{ padding: '12px 4px' }}>
          <span
            className="dash-empty__icon"
            style={{ color: 'var(--c-green)', background: 'var(--t-green)' }}
          >
            <CheckCircleOutlined />
          </span>
          <Typography.Paragraph style={{ margin: '10px 0 0' }}>
            No delays, breakdowns or emergencies today.
          </Typography.Paragraph>
        </div>
      ) : (
        <div className="dash-list">
          {sorted.slice(0, MAX).map((a) => {
            const s = severity(a)
            return (
              <button
                type="button"
                key={`${a.kind}-${a.id}`}
                className="dash-item dash-item--button"
                onClick={() => navigate('/alerts')}
                aria-label={`${s.label}: ${a.route_code}, ${a.bus_number}. Open alerts`}
              >
                <span className={`icon-tile icon-tile--${s.tone}`} aria-hidden>
                  {s.icon}
                </span>
                <span className="dash-item__body">
                  <span>
                    <span className={`pill pill--${s.tone}`} style={{ marginRight: 8 }}>
                      {s.label}
                    </span>
                    <Typography.Text strong>
                      {a.route_code} · {a.bus_number}
                    </Typography.Text>
                  </span>
                  <Typography.Text style={{ fontSize: 13 }} ellipsis>
                    {a.kind === 'emergency'
                      ? a.message
                      : `${delayReasonLabels[a.reason!]}${a.note ? `: ${a.note}` : ''}`}
                  </Typography.Text>
                  <span className="dash-item__meta">
                    {clockTime(a.created_at)} · {tripTypeLabels[a.trip_type]} · {a.driver_name}
                  </span>
                </span>
              </button>
            )
          })}
          {sorted.length > MAX && (
            <Typography.Text type="secondary" style={{ fontSize: 12, padding: '8px 4px 0' }}>
              {sorted.length - MAX} more on the Delays &amp; Alerts page.
            </Typography.Text>
          )}
        </div>
      )}
    </Card>
  )
}
