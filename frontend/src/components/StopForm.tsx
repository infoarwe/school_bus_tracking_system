import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  App,
  Col,
  Collapse,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Row,
  Select,
  Tag,
  Typography,
} from 'antd'
import { CheckCircleFilled, EnvironmentOutlined } from '@ant-design/icons'
import LocationSearch from './LocationSearch'
import RouteMap from './RouteMap'
import { routesApi } from '../services/transport'
import { trackingSettingsApi } from '../services/settings'
import type { BusRoute, Stop, StopInput } from '../services/types'
import { applyApiErrors } from '../utils/formErrors'
import { centreOf, distanceM, formatDistance } from '../utils/geo'

interface Values {
  name: string
  landmark?: string
  latitude?: number
  longitude?: number
  pickup_time?: string
  drop_time?: string
  geofence_radius_m: number
  position?: number
}

/** Stops closer than this to another stop of the route are flagged (not blocked). */
const NEAR_M = 50

/**
 * Add or edit a stop: search for the place (or click the map), adjust by dragging the
 * marker, then set the times. Coordinates can still be typed under "Advanced".
 */
export default function StopForm({
  schoolId,
  route,
  stops,
  stop,
  onClose,
  onSaved,
}: {
  schoolId: string
  route: BusRoute
  stops: Stop[]
  stop: Stop | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<Values>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)
  const [locationMissing, setLocationMissing] = useState(false)
  const latitude = Form.useWatch('latitude', form)
  const longitude = Form.useWatch('longitude', form)
  const radius = Form.useWatch('geofence_radius_m', form) ?? 100
  const others = useMemo(() => stops.filter((s) => s.id !== stop?.id), [stops, stop])
  const placed = latitude != null && longitude != null
  const picked = useMemo(
    () => (placed ? { latitude, longitude, radius } : null),
    [placed, latitude, longitude, radius],
  )

  // New stops start with the school's default arrival radius (Settings → Live tracking).
  useEffect(() => {
    if (stop) return
    trackingSettingsApi
      .get(schoolId)
      .then((t) => {
        if (!form.isFieldTouched('geofence_radius_m')) {
          form.setFieldValue('geofence_radius_m', t.default_geofence_m)
        }
      })
      .catch(() => {}) // keep 100 m
  }, [schoolId, stop, form])

  const nearest = useMemo(() => {
    if (!picked) return null
    let best: { stop: Stop; d: number } | null = null
    for (const s of others) {
      const d = distanceM(picked, s)
      if (!best || d < best.d) best = { stop: s, d }
    }
    return best
  }, [picked, others])

  function place(lat: number, lng: number) {
    form.setFieldsValue({ latitude: lat, longitude: lng })
    form.setFields([
      { name: 'latitude', errors: [] },
      { name: 'longitude', errors: [] },
    ])
    setLocationMissing(false)
  }

  async function save(v: Values) {
    setSaving(true)
    try {
      const input: StopInput = {
        name: v.name.trim(),
        landmark: (v.landmark ?? '').trim(),
        latitude: v.latitude!,
        longitude: v.longitude!,
        pickup_time: v.pickup_time || null,
        drop_time: v.drop_time || null,
        geofence_radius_m: v.geofence_radius_m,
      }
      if (stop) await routesApi.updateStop(schoolId, route.id, stop.id, input)
      else await routesApi.createStop(schoolId, route.id, { ...input, position: v.position })
      message.success(stop ? `${input.name} updated.` : `${input.name} added to ${route.code}.`)
      onSaved()
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open
      title={stop ? `Edit stop: ${stop.name}` : `Add a stop to ${route.code} · ${route.name}`}
      okText={stop ? 'Save changes' : 'Add stop'}
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      width={{ xs: '100%', lg: 1080 }}
      destroyOnHidden
      mask={{ closable: false }}
    >
      <Row gutter={[20, 16]}>
        <Col xs={24} lg={10}>
          <Form
            form={form}
            layout="vertical"
            requiredMark="optional"
            initialValues={
              stop
                ? { ...stop, pickup_time: stop.pickup_time ?? '', drop_time: stop.drop_time ?? '' }
                : { geofence_radius_m: 100 }
            }
            onFinish={save}
            onFinishFailed={({ errorFields }) => {
              if (errorFields.some((f) => f.name[0] === 'latitude' || f.name[0] === 'longitude')) {
                setLocationMissing(true)
              }
            }}
            scrollToFirstError
          >
            <Form.Item label="Find the place" style={{ marginBottom: 12 }}>
              <LocationSearch
                autoFocus={!stop}
                near={centreOf(others)}
                onSelect={(p) => {
                  form.setFieldsValue({
                    name: form.getFieldValue('name') || p.name.slice(0, 200),
                    landmark: p.address.slice(0, 200),
                  })
                  place(p.latitude, p.longitude)
                }}
              />
            </Form.Item>

            <div style={{ marginBottom: 16 }} aria-live="polite">
              <LocationStatus placed={placed} missing={locationMissing && !placed} />
            </div>

            <Form.Item
              name="name"
              label="Stop name"
              tooltip="What parents and drivers see, e.g. a landmark."
              rules={[
                { required: true, whitespace: true, message: 'Give the stop a name.' },
                { max: 200 },
                {
                  validator: (_, v: string | undefined) =>
                    v && others.some((s) => s.name.trim().toLowerCase() === v.trim().toLowerCase())
                      ? Promise.reject(new Error('This route already has a stop with this name.'))
                      : Promise.resolve(),
                },
              ]}
            >
              <Input placeholder="e.g. Gandhipuram Bus Stand" />
            </Form.Item>
            <Form.Item name="landmark" label="Address / landmark" rules={[{ max: 200 }]}>
              <Input.TextArea
                autoSize={{ minRows: 1, maxRows: 3 }}
                placeholder="Filled in by search"
              />
            </Form.Item>

            {!stop && others.length > 0 && (
              <Form.Item name="position" label="Position on the route">
                <Select
                  allowClear
                  placeholder={`At the end (after stop ${others.length})`}
                  options={others.map((s) => ({
                    value: s.sequence,
                    label: `Before ${s.sequence}. ${s.name}`,
                  }))}
                />
              </Form.Item>
            )}

            <Row gutter={12}>
              {route.supports_pickup && (
                <Col span={12}>
                  <Form.Item
                    name="pickup_time"
                    label="Pickup time"
                    tooltip="Morning Pickup: when the bus is due here."
                  >
                    <Input type="time" />
                  </Form.Item>
                </Col>
              )}
              {route.supports_drop && (
                <Col span={12}>
                  <Form.Item
                    name="drop_time"
                    label="Drop time"
                    tooltip="Evening Drop: when the bus is due here."
                  >
                    <Input type="time" />
                  </Form.Item>
                </Col>
              )}
            </Row>
            <Form.Item
              name="geofence_radius_m"
              label="Arrival radius"
              tooltip="The bus counts as Reached when it is inside this circle around the stop."
              rules={[{ required: true }]}
            >
              <InputNumber min={25} max={1000} step={25} suffix="m" style={{ width: 160 }} />
            </Form.Item>

            <Collapse
              size="small"
              ghost
              items={[
                {
                  key: 'coords',
                  label: 'Advanced: enter coordinates',
                  // Rendered while collapsed so the coordinate fields are always part of the form.
                  forceRender: true,
                  children: (
                    <Row gutter={8}>
                      <Col span={12}>
                        <Form.Item
                          name="latitude"
                          label="Latitude"
                          rules={[{ required: true, message: '' }]}
                          style={{ marginBottom: 0 }}
                        >
                          <InputNumber min={-90} max={90} step={0.0001} style={{ width: '100%' }} />
                        </Form.Item>
                      </Col>
                      <Col span={12}>
                        <Form.Item
                          name="longitude"
                          label="Longitude"
                          rules={[{ required: true, message: '' }]}
                          style={{ marginBottom: 0 }}
                        >
                          <InputNumber
                            min={-180}
                            max={180}
                            step={0.0001}
                            style={{ width: '100%' }}
                          />
                        </Form.Item>
                      </Col>
                    </Row>
                  ),
                },
              ]}
            />
          </Form>
        </Col>
        <Col xs={24} lg={14}>
          <Flex vertical gap={8}>
            <Typography.Text type="secondary">
              <EnvironmentOutlined /> Click the map to place the stop, or drag the{' '}
              <Tag color="blue" style={{ marginInline: 2 }}>
                ★
              </Tag>
              marker to adjust it. Numbered circles are the route's other stops.
            </Typography.Text>
            <RouteMap stops={others} picked={picked} onPick={place} height={460} />
            {nearest && nearest.d < NEAR_M && (
              <Alert
                type="warning"
                showIcon
                title={`Only ${formatDistance(nearest.d)} from stop ${nearest.stop.sequence}. ${nearest.stop.name}.`}
                description="Stops this close can be confused by GPS. Use one stop, or check the position."
              />
            )}
          </Flex>
        </Col>
      </Row>
    </Modal>
  )
}

function LocationStatus({ placed, missing }: { placed: boolean; missing: boolean }) {
  if (placed) {
    return (
      <Typography.Text type="success">
        <CheckCircleFilled /> Location set. Drag the marker on the map to fine-tune it.
      </Typography.Text>
    )
  }
  return missing ? (
    <Alert
      type="error"
      showIcon
      title="Choose the stop's location: search above, or click the map."
    />
  ) : (
    <Typography.Text type="secondary">
      No location yet: search above, or click the map.
    </Typography.Text>
  )
}
