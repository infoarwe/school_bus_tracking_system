import { useEffect, useState } from 'react'
import { Alert, App, Form, Modal, Select, Spin } from 'antd'
import { studentsApi } from '../services/people'
import { routesApi } from '../services/transport'
import type { BusRoute, Stop, Student } from '../services/types'
import { applyApiErrors } from '../utils/formErrors'

interface Values {
  route_id: string
  pickup_stop_id?: string
  drop_stop_id?: string
}

/**
 * Assign a student to a route and stops. The stop lists only ever contain the
 * selected route's stops (CLAUDE.md rule 5); the API enforces the same.
 */
export default function AssignRouteModal({
  schoolId,
  student,
  onClose,
  onSaved,
}: {
  schoolId: string
  student: Student
  onClose: () => void
  onSaved: () => void
}) {
  const { message } = App.useApp()
  const [form] = Form.useForm<Values>()
  const [routes, setRoutes] = useState<BusRoute[] | null>(null)
  const [route, setRoute] = useState<BusRoute | null>(null)
  const [stops, setStops] = useState<Stop[]>([])
  const [saving, setSaving] = useState(false)
  const routeId = Form.useWatch('route_id', form)

  useEffect(() => {
    routesApi
      .list(schoolId, { status: 'active', page_size: 100 })
      .then((res) => setRoutes(res.data))
      .catch(() => setRoutes([]))
  }, [schoolId])

  // Load the chosen route's stops; clear stops that belong to a different route.
  useEffect(() => {
    if (!routeId) return
    let cancelled = false
    routesApi.get(schoolId, routeId).then((r) => {
      if (cancelled) return
      setRoute(r)
      setStops(r.stops ?? [])
      const ids = new Set((r.stops ?? []).map((s) => s.id))
      const { pickup_stop_id, drop_stop_id } = form.getFieldsValue()
      form.setFieldsValue({
        pickup_stop_id: pickup_stop_id && ids.has(pickup_stop_id) ? pickup_stop_id : undefined,
        drop_stop_id: drop_stop_id && ids.has(drop_stop_id) ? drop_stop_id : undefined,
      })
    })
    return () => {
      cancelled = true
    }
  }, [schoolId, routeId, form])

  async function save(v: Values) {
    setSaving(true)
    try {
      await studentsApi.assign(schoolId, student.id, {
        route_id: v.route_id,
        pickup_stop_id: route?.supports_pickup ? (v.pickup_stop_id ?? null) : null,
        drop_stop_id: route?.supports_drop ? (v.drop_stop_id ?? null) : null,
      })
      message.success(`${student.name} assigned to ${route?.code}.`)
      onSaved()
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const stopOptions = stops.map((s) => ({
    value: s.id,
    label: `${s.sequence}. ${s.name}`,
  }))
  const current = student.assignment

  return (
    <Modal
      open
      title={`Route & stop: ${student.name}`}
      okText="Save"
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      destroyOnHidden
    >
      {student.transport_status === 'not_using' && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          title="This student is marked as not using school transport. Edit the student first."
        />
      )}
      {routes === null ? (
        <Spin />
      ) : (
        <Form
          form={form}
          layout="vertical"
          onFinish={save}
          initialValues={{
            route_id: current?.route_id,
            pickup_stop_id: current?.pickup_stop?.id,
            drop_stop_id: current?.drop_stop?.id,
          }}
          onValuesChange={(changed: Partial<Values>) => {
            // Most students get on and off at the same stop: default drop to pickup.
            if (changed.pickup_stop_id && !form.getFieldValue('drop_stop_id')) {
              form.setFieldValue('drop_stop_id', changed.pickup_stop_id)
            }
          }}
        >
          <Form.Item name="route_id" label="Route" rules={[{ required: true }]}>
            <Select
              showSearch={{ optionFilterProp: 'label' }}
              placeholder="Choose a route"
              options={routes.map((r) => ({
                value: r.id,
                label: `${r.code} · ${r.name}`,
                disabled: r.stop_count === 0,
              }))}
            />
          </Form.Item>
          {route && route.id === routeId && (
            <>
              {route.supports_pickup && (
                <Form.Item
                  name="pickup_stop_id"
                  label="Pickup stop (Morning Pickup)"
                  rules={[{ required: true }]}
                >
                  <Select options={stopOptions} placeholder="Stop of this route" />
                </Form.Item>
              )}
              {route.supports_drop && (
                <Form.Item
                  name="drop_stop_id"
                  label="Drop stop (Evening Drop)"
                  rules={[{ required: true }]}
                >
                  <Select options={stopOptions} placeholder="Stop of this route" />
                </Form.Item>
              )}
            </>
          )}
        </Form>
      )}
    </Modal>
  )
}
