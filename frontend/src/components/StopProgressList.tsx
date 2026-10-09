import { Tag, Timeline, Typography } from 'antd'
import type { StopProgress, StopStatus } from '../services/types'
import { formatDistance, formatEta } from '../utils/format'

const statusColors: Record<StopStatus, string> = {
  upcoming: 'default',
  approaching: 'orange',
  reached: 'green',
  crossed: 'blue',
}

const time = (iso: string | null) =>
  iso ? new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : ''

/** Stops in travel order with their live status (computed from GPS) and ETA. */
export default function StopProgressList({ stops }: { stops: StopProgress[] }) {
  return (
    <Timeline
      items={stops.map((s) => ({
        color:
          s.status === 'crossed'
            ? s.missed
              ? 'red'
              : 'blue'
            : s.status === 'reached'
              ? 'green'
              : 'gray',
        content: (
          <>
            <strong>{s.stop_id === 'school' ? 'School' : `${s.sequence}. ${s.name}`}</strong>{' '}
            <Tag color={s.missed ? 'red' : statusColors[s.status]}>
              {s.missed ? 'missed' : s.status}
            </Tag>
            <br />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {s.scheduled_time && `Scheduled ${s.scheduled_time}`}
              {s.reached_at && ` · reached ${time(s.reached_at)}`}
              {s.crossed_at && ` · left ${time(s.crossed_at)}`}
              {s.eta_seconds != null &&
                ` · ETA ${formatEta(s.eta_seconds)} (${formatDistance(s.distance_m)})`}
            </Typography.Text>
          </>
        ),
      }))}
    />
  )
}
