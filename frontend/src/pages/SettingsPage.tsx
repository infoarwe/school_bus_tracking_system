import { useCallback, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  Flex,
  Form,
  Input,
  InputNumber,
  Typography,
} from 'antd'
import BrandingSettingsCard from '../components/BrandingSettingsCard'
import LoadingOrError from '../components/LoadingOrError'
import PageHeader from '../components/PageHeader'
import PushSettingsCard from '../components/PushSettingsCard'
import RequireSchool from '../components/RequireSchool'
import RouteMap from '../components/RouteMap'
import { useLoad } from '../hooks/useLoad'
import { setCachedMapsKey } from '../hooks/useSchoolMapsKey'
import { settingsApi, trackingSettingsApi, type TrackingSettings } from '../services/settings'
import { applyApiErrors } from '../utils/formErrors'

// School settings. Sprint 8 adds geofence defaults, retention and more; maps keys come first
// because every school uses its own Google Maps keys.
export default function SettingsPage() {
  return (
    <RequireSchool>
      {(schoolId, schoolName) => (
        <Flex vertical gap="middle">
          <PageHeader title="Settings" subtitle={schoolName} />
          <MapsSettingsCard schoolId={schoolId} />
          <TrackingSettingsCard schoolId={schoolId} />
          <PushSettingsCard schoolId={schoolId} />
          <BrandingSettingsCard schoolId={schoolId} />
        </Flex>
      )}
    </RequireSchool>
  )
}

interface Values {
  browser_key: string
  server_key: string
  remove_server_key: boolean
}

function MapsSettingsCard({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const [form] = Form.useForm<Values>()
  const loadMaps = useCallback(() => settingsApi.getMaps(schoolId), [schoolId])
  const { data: current, setData: setCurrent, error: loadError, retry } = useLoad(loadMaps)
  const [saving, setSaving] = useState(false)
  const removeServerKey = Form.useWatch('remove_server_key', form)

  async function save(v: Values) {
    setSaving(true)
    try {
      const saved = await settingsApi.updateMaps(schoolId, {
        browser_key: v.browser_key ?? '',
        // Leave the stored server key alone unless a new one is typed or removal is ticked.
        server_key: v.remove_server_key ? '' : v.server_key || undefined,
      })
      setCurrent(saved)
      setCachedMapsKey(schoolId, saved.browser_key)
      form.setFieldsValue({ server_key: '', remove_server_key: false })
      message.success('Maps settings saved. Open a route to check the map.')
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card title="Google Maps">
      {!current ? (
        <LoadingOrError error={loadError} onRetry={retry} />
      ) : (
        <Form
          form={form}
          layout="vertical"
          style={{ maxWidth: 640 }}
          initialValues={{
            browser_key: current.browser_key,
            server_key: '',
            remove_server_key: false,
          }}
          onFinish={save}
        >
          <Typography.Paragraph type="secondary">
            Each school uses its own Google Maps keys from its Google Cloud project. Without a
            browser key the admin maps use OpenStreetMap.
          </Typography.Paragraph>

          <Form.Item
            name="browser_key"
            label="Browser key (maps in the admin web)"
            extra="Enable the Maps JavaScript API. In Google Cloud, restrict it to your admin web's domain (HTTP referrers). All staff of this school can see this key."
          >
            <Input placeholder="AIza…" allowClear autoComplete="off" />
          </Form.Item>

          <Form.Item
            name="server_key"
            label="Server key (ETA and distance)"
            extra={
              <>
                Used only by the server to calculate ETA (Routes API). Restrict it to the server's
                IP address. It is stored encrypted and never shown again.{' '}
                {current.server_key_set
                  ? `A key ending in ${current.server_key_hint.replace('…', '')} is saved; leave empty to keep it.`
                  : 'No server key saved yet.'}
              </>
            }
          >
            <Input.Password
              placeholder={current.server_key_set ? `Saved (${current.server_key_hint})` : 'AIza…'}
              autoComplete="new-password"
              disabled={removeServerKey}
            />
          </Form.Item>
          {current.server_key_set && (
            <Form.Item name="remove_server_key" valuePropName="checked">
              <Checkbox>Remove the saved server key</Checkbox>
            </Form.Item>
          )}
          {!current.browser_key && (
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 16 }}
              title="Maps currently use OpenStreetMap for this school."
            />
          )}
          <Button type="primary" htmlType="submit" loading={saving}>
            Save
          </Button>
        </Form>
      )}
    </Card>
  )
}

function TrackingSettingsCard({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const [form] = Form.useForm<TrackingSettings>()
  const loadTracking = useCallback(() => trackingSettingsApi.get(schoolId), [schoolId])
  const { data: current, setData: setCurrent, error: loadError, retry } = useLoad(loadTracking)
  const [saving, setSaving] = useState(false)
  const schoolLat = Form.useWatch('school_latitude', form)
  const schoolLng = Form.useWatch('school_longitude', form)

  async function save(v: TrackingSettings) {
    setSaving(true)
    try {
      setCurrent(
        await trackingSettingsApi.update(schoolId, {
          ...v,
          school_latitude: v.school_latitude ?? null,
          school_longitude: v.school_longitude ?? null,
        }),
      )
      message.success('Tracking settings saved.')
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card title="Live tracking">
      {!current ? (
        <LoadingOrError error={loadError} onRetry={retry} />
      ) : (
        <Form
          form={form}
          layout="vertical"
          style={{ maxWidth: 640 }}
          initialValues={current}
          onFinish={save}
        >
          <Form.Item
            name="location_retention_days"
            label="Keep GPS history for (days)"
            extra="Used for trip replay and reports. 0 keeps no history; live tracking still works. Older points are deleted automatically."
            rules={[{ required: true }]}
          >
            <InputNumber min={0} max={365} style={{ width: 160 }} />
          </Form.Item>
          <Form.Item
            name="stale_after_seconds"
            label="Show a bus as offline after (seconds without GPS)"
            rules={[{ required: true }]}
          >
            <InputNumber min={30} max={1800} step={30} style={{ width: 160 }} />
          </Form.Item>
          <Form.Item
            name="default_geofence_m"
            label="Default arrival radius for new stops (metres)"
            extra="Each stop can still have its own radius on the route page."
            rules={[{ required: true }]}
          >
            <InputNumber min={25} max={1000} step={25} style={{ width: 160 }} />
          </Form.Item>
          <Form.Item
            name="approach_distance_m"
            label='"Approaching" alert distance (metres)'
            extra="The stop shows as approaching, and parents are alerted, when the bus is this close."
            rules={[{ required: true }]}
          >
            <InputNumber min={100} max={5000} step={100} style={{ width: 160 }} />
          </Form.Item>
          <Form.Item
            label="School location"
            extra='Click the map. Morning Pickup trips end here ("School Reached"). Leave empty to skip that alert.'
          >
            <Flex gap="small" style={{ marginBottom: 8 }}>
              <Form.Item name="school_latitude" noStyle>
                <InputNumber
                  placeholder="Latitude"
                  min={-90}
                  max={90}
                  step={0.0001}
                  style={{ width: 160 }}
                />
              </Form.Item>
              <Form.Item name="school_longitude" noStyle>
                <InputNumber
                  placeholder="Longitude"
                  min={-180}
                  max={180}
                  step={0.0001}
                  style={{ width: 160 }}
                />
              </Form.Item>
              <Button
                onClick={() =>
                  form.setFieldsValue({ school_latitude: null, school_longitude: null })
                }
              >
                Clear
              </Button>
            </Flex>
            <RouteMap
              stops={[]}
              height={300}
              picked={
                schoolLat != null && schoolLng != null
                  ? { latitude: schoolLat, longitude: schoolLng, radius: 150 }
                  : null
              }
              onPick={(lat, lng) =>
                form.setFieldsValue({ school_latitude: lat, school_longitude: lng })
              }
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={saving}>
            Save
          </Button>
        </Form>
      )}
    </Card>
  )
}
