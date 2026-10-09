import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Flex, Modal, Segmented, Slider, Spin, Typography } from 'antd'
import { PauseOutlined, CaretRightOutlined } from '@ant-design/icons'
import RouteMap from './RouteMap'
import { trackApi } from '../services/admin'
import type { Stop, TrackPoint } from '../services/types'

/**
 * Replays a trip's stored GPS track on the route map: play/pause, scrub, speed.
 * Needs GPS history (Settings → Live tracking → keep history for N days).
 */
export default function TripReplay({
  schoolId,
  tripId,
  title,
  stops,
  onClose,
}: {
  schoolId: string
  tripId: string
  title: string
  stops: Stop[]
  onClose: () => void
}) {
  const [points, setPoints] = useState<TrackPoint[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [index, setIndex] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [speed, setSpeed] = useState(10)

  useEffect(() => {
    trackApi
      .get(schoolId, tripId)
      .then(setPoints)
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : 'Could not load the GPS track'),
      )
  }, [schoolId, tripId])

  const times = useMemo(
    () => (points ?? []).map((p) => new Date(p.recorded_at).getTime()),
    [points],
  )

  // Advance at `speed`× real time, following the gaps between fixes.
  useEffect(() => {
    if (!playing || !points || index >= points.length - 1) return
    const gap = Math.min(5000, Math.max(50, (times[index + 1] - times[index]) / speed))
    const t = setTimeout(() => setIndex((i) => i + 1), gap)
    return () => clearTimeout(t)
  }, [playing, index, points, times, speed])

  const track = useMemo(
    () => (points ?? []).map((p) => ({ lat: p.latitude, lng: p.longitude })),
    [points],
  )
  const at = points?.[index]
  const atEnd = !!points && index >= points.length - 1

  return (
    <Modal
      open
      title={`Replay: ${title}`}
      onCancel={onClose}
      footer={null}
      width={960}
      destroyOnHidden
    >
      {error && <Alert type="error" showIcon title={error} />}
      {!points && !error && <Spin style={{ display: 'block', margin: 40 }} />}
      {points && points.length === 0 && (
        <Alert
          type="info"
          showIcon
          title="No GPS history for this trip."
          description="Either the bus sent no GPS, or the school keeps no history (Settings → Live tracking)."
        />
      )}
      {points && points.length > 0 && (
        <Flex vertical gap="small">
          <RouteMap
            stops={stops}
            track={track}
            bus={at ? { lat: at.latitude, lng: at.longitude } : null}
            height={460}
          />
          <Flex gap="middle" align="center" wrap>
            <Button
              type="primary"
              icon={playing && !atEnd ? <PauseOutlined /> : <CaretRightOutlined />}
              onClick={() => {
                if (atEnd) setIndex(0)
                setPlaying(!(playing && !atEnd))
              }}
            >
              {playing && !atEnd ? 'Pause' : atEnd ? 'Replay' : 'Play'}
            </Button>
            <Segmented
              value={speed}
              onChange={(v) => setSpeed(v as number)}
              options={[
                { value: 5, label: '5×' },
                { value: 10, label: '10×' },
                { value: 30, label: '30×' },
                { value: 60, label: '60×' },
              ]}
            />
            <Typography.Text>
              {at && new Date(at.recorded_at).toLocaleTimeString()}
              {at?.speed_mps != null && ` · ${Math.round(at.speed_mps * 3.6)} km/h`}
            </Typography.Text>
            <Typography.Text type="secondary">{points.length} GPS points</Typography.Text>
          </Flex>
          <Slider
            min={0}
            max={points.length - 1}
            value={index}
            onChange={(v) => {
              setPlaying(false)
              setIndex(v)
            }}
            tooltip={{
              formatter: (v) =>
                v != null ? new Date(points[v].recorded_at).toLocaleTimeString() : '',
            }}
          />
        </Flex>
      )}
    </Modal>
  )
}
