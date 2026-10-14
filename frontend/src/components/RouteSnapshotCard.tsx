import { useEffect, useState } from 'react'
import { Alert, App, Button, Flex, Image, Skeleton, Typography } from 'antd'
import { DownloadOutlined, ReloadOutlined } from '@ant-design/icons'
import { ApiError } from '../services/api'
import { tripsApi } from '../services/trips'
import type { RouteSnapshot } from '../services/types'
import { errorMessage } from '../utils/formErrors'
import { formatDistance } from '../utils/geo'

function duration(from: string | null, to: string | null) {
  if (!from || !to) return null
  const min = Math.round((new Date(to).getTime() - new Date(from).getTime()) / 60000)
  return min < 60 ? `${min} min` : `${Math.floor(min / 60)} h ${min % 60} min`
}

/**
 * The travelled route of a completed trip: the map image saved when the trip
 * ended (planned route blue, travelled green), with distance and duration.
 */
export default function RouteSnapshotCard({
  schoolId,
  tripId,
  fileName,
}: {
  schoolId: string
  tripId: string
  fileName: string
}) {
  const { message } = App.useApp()
  const [snap, setSnap] = useState<RouteSnapshot | null | undefined>(undefined) // null = none yet
  const [image, setImage] = useState<string | null>(null)
  const [making, setMaking] = useState(false)

  useEffect(() => {
    let cancelled = false
    tripsApi
      .routeSnapshot(schoolId, tripId)
      .then((s) => !cancelled && setSnap(s))
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 404) setSnap(null)
        else message.error(errorMessage(e))
      })
    return () => {
      cancelled = true
    }
  }, [schoolId, tripId, message])

  // Load the PNG (it needs the access token) whenever a new snapshot is ready.
  useEffect(() => {
    if (!snap?.has_image) return
    let url: string | null = null
    let cancelled = false
    tripsApi
      .routeSnapshotImage(schoolId, tripId)
      .then((blob) => {
        if (cancelled) return
        url = URL.createObjectURL(blob)
        setImage(url)
      })
      .catch(() => !cancelled && setImage(null))
    return () => {
      cancelled = true
      if (url) URL.revokeObjectURL(url)
    }
  }, [schoolId, tripId, snap?.has_image, snap?.updated_at])

  const make = async () => {
    setMaking(true)
    try {
      setSnap(await tripsApi.makeRouteSnapshot(schoolId, tripId))
    } catch (e) {
      message.error(errorMessage(e))
    } finally {
      setMaking(false)
    }
  }

  const remake = (label: string) => (
    <Button size="small" icon={<ReloadOutlined />} loading={making} onClick={make}>
      {label}
    </Button>
  )

  return (
    <div>
      <Flex align="center" justify="space-between" gap="small" wrap>
        <Typography.Title level={5} style={{ margin: 0 }}>
          Travelled route
        </Typography.Title>
        {snap?.has_image && image && (
          <Button size="small" icon={<DownloadOutlined />} href={image} download={fileName}>
            Download
          </Button>
        )}
      </Flex>
      <div style={{ marginTop: 8 }}>
        {snap === undefined && <Skeleton.Image active style={{ width: '100%', height: 200 }} />}
        {snap === null && (
          <Alert
            type="info"
            showIcon
            title="No route image yet."
            description="It is made a few seconds after the trip is completed."
            action={remake('Make now')}
          />
        )}
        {snap && (
          <Flex vertical gap="small">
            {snap.has_image &&
              (image ? (
                <Image
                  src={image}
                  alt="Map of the route the bus travelled"
                  style={{ width: '100%', borderRadius: 8 }}
                />
              ) : (
                <Skeleton.Image active style={{ width: '100%', height: 200 }} />
              ))}
            {snap.status !== 'ready' && (
              <Alert
                type={snap.status === 'no_gps' ? 'info' : 'warning'}
                showIcon
                title={snap.status === 'no_gps' ? 'No GPS history' : 'No route image'}
                description={snap.error}
                action={snap.status === 'failed' ? remake('Try again') : undefined}
              />
            )}
            {snap.point_count > 1 && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                Travelled {formatDistance(snap.distance_m)}
                {duration(snap.started_at, snap.ended_at) &&
                  ` in ${duration(snap.started_at, snap.ended_at)}`}{' '}
                · {snap.point_count} GPS points · blue: planned route, green: travelled, A: start,
                B: end
              </Typography.Text>
            )}
          </Flex>
        )}
      </div>
    </div>
  )
}
