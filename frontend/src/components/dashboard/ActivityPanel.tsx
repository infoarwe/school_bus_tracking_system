import { useNavigate } from 'react-router-dom'
import { Button, Card, Typography } from 'antd'
import { ArrowRightOutlined, HistoryOutlined } from '@ant-design/icons'
import { roleLabels, type AuditRow, type Role } from '../../services/types'
import { timeAgo } from './time'

// "trip.status_change" → "Trip status changed". Generic, so new audit actions still read well.
const verbs: Record<string, string> = {
  create: 'created',
  update: 'updated',
  delete: 'deleted',
  cancel: 'cancelled',
  override: 'overridden',
  assign: 'assigned',
  unassign: 'unassigned',
  status: 'status changed',
  status_change: 'status changed',
  reorder: 'reordered',
  import: 'imported',
  export: 'exported',
}

function describe(row: AuditRow): string {
  const [entity, ...rest] = row.action.split('.')
  const verb = rest.join('.')
  const subject = entity.replaceAll('_', ' ')
  const did = verbs[verb] ?? verb.replaceAll('_', ' ')
  return `${subject[0].toUpperCase()}${subject.slice(1)} ${did}`.trim()
}

/** Logins and similar are noise on a dashboard; the audit log page still has them. */
const isNoise = (row: AuditRow) => row.action.startsWith('auth.')

/** Recent changes, straight from the audit log (no invented activity). */
export default function ActivityPanel({ rows }: { rows: AuditRow[] | null }) {
  const navigate = useNavigate()
  const shown = (rows ?? []).filter((r) => !isNoise(r)).slice(0, 6)

  return (
    <Card
      className="dash-panel"
      title="Recent activity"
      extra={
        <Button type="link" onClick={() => navigate('/audit-logs')} style={{ paddingInline: 0 }}>
          Audit log <ArrowRightOutlined />
        </Button>
      }
    >
      {rows === null ? (
        <Typography.Text type="secondary">Activity could not be loaded.</Typography.Text>
      ) : shown.length === 0 ? (
        <div className="dash-empty" style={{ padding: '12px 4px' }}>
          <HistoryOutlined className="dash-empty__icon" />
          <Typography.Paragraph style={{ margin: '6px 0 0' }}>
            No recent changes.
          </Typography.Paragraph>
        </div>
      ) : (
        <div className="dash-list">
          {shown.map((r) => (
            <div key={r.id} className="dash-item">
              <span className="dash-dot" style={{ background: '#cbd5e1' }} />
              <span className="dash-item__body">
                <Typography.Text style={{ fontSize: 13 }}>{describe(r)}</Typography.Text>
                <span className="dash-item__meta">
                  {r.actor_name ?? 'System'}
                  {r.actor_role && ` (${roleLabels[r.actor_role as Role] ?? r.actor_role})`} ·{' '}
                  {timeAgo(r.created_at)}
                </span>
              </span>
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}
