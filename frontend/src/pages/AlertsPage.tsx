import { useEffect, useState } from 'react'
import { App, Button, Card, Flex, Form, Input, InputNumber, Modal, Select } from 'antd'
import { WarningOutlined } from '@ant-design/icons'
import AlertList from '../components/AlertList'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import { useAlerts } from '../context/AlertsContext'
import { useCurrentSchool } from '../context/SchoolContext'
import { alertsApi } from '../services/alerts'
import { tripsApi } from '../services/trips'
import { delayReasonLabels, tripTypeLabels, type DelayReason, type Trip } from '../services/types'
import { todayIn } from '../utils/dates'
import { applyApiErrors } from '../utils/formErrors'

export default function AlertsPage() {
  return <RequireSchool>{(schoolId) => <Alerts schoolId={schoolId} />}</RequireSchool>
}

function Alerts({ schoolId }: { schoolId: string }) {
  const [reporting, setReporting] = useState(false)
  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Delays & Alerts"
        subtitle="Emergencies from drivers appear here instantly. Reported delays notify the parents on that trip."
        extra={
          <Button icon={<WarningOutlined />} onClick={() => setReporting(true)}>
            Report a delay
          </Button>
        }
      />
      <Card>
        <AlertList schoolId={schoolId} />
      </Card>
      {reporting && <ReportDelay schoolId={schoolId} onClose={() => setReporting(false)} />}
    </Flex>
  )
}

interface DelayValues {
  trip_id: string
  minutes: number
  reason: DelayReason
  note?: string
}

function ReportDelay({ schoolId, onClose }: { schoolId: string; onClose: () => void }) {
  const { message } = App.useApp()
  const { school } = useCurrentSchool()
  const { reload } = useAlerts()
  const [form] = Form.useForm<DelayValues>()
  const [trips, setTrips] = useState<Trip[]>([])
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    tripsApi
      .list(schoolId, { date: todayIn(school?.timezone), page_size: 100 })
      .then((r) =>
        setTrips(r.data.filter((t) => t.status !== 'completed' && t.status !== 'cancelled')),
      )
      .catch(() => setTrips([]))
  }, [schoolId, school?.timezone])

  async function save(v: DelayValues) {
    setSaving(true)
    try {
      await alertsApi.reportDelay(schoolId, v.trip_id, {
        minutes: v.minutes,
        reason: v.reason,
        note: v.note,
      })
      message.success('Delay recorded. Parents on this trip have been notified.')
      reload()
      onClose()
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
      title="Report a delay"
      okText="Notify parents"
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      destroyOnHidden
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={save}
        initialValues={{ minutes: 15, reason: 'traffic' }}
      >
        <Form.Item name="trip_id" label="Today's trip" rules={[{ required: true }]}>
          <Select
            options={trips.map((t) => ({
              value: t.id,
              label: `${t.route.code} · ${tripTypeLabels[t.trip_type]} · ${t.bus.vehicle_number} (${t.status})`,
            }))}
            notFoundContent="No open trips today"
          />
        </Form.Item>
        <Flex gap="middle">
          <Form.Item name="minutes" label="Delay (minutes)" rules={[{ required: true }]}>
            <InputNumber min={1} max={240} step={5} />
          </Form.Item>
          <Form.Item name="reason" label="Reason" rules={[{ required: true }]} style={{ flex: 1 }}>
            <Select
              options={Object.entries(delayReasonLabels).map(([value, label]) => ({
                value,
                label,
              }))}
            />
          </Form.Item>
        </Flex>
        <Form.Item name="note" label="Note (staff only)">
          <Input.TextArea rows={2} maxLength={500} />
        </Form.Item>
      </Form>
    </Modal>
  )
}
