import { useEffect, useMemo, useState } from 'react'
import { Alert, App, Checkbox, Flex, Form, Input, Modal, Select, Typography } from 'antd'
import { routesApi } from '../services/transport'
import { tripsApi } from '../services/trips'
import {
  tripTypeLabels,
  type BulkTripsInput,
  type Bus,
  type BusRoute,
  type Driver,
  type RouteSchedule,
  type WeekDay,
} from '../services/types'
import { addDays, formatDate, weekdayOf } from '../utils/dates'
import { applyApiErrors } from '../utils/formErrors'

/** The server's limit: trips can be planned up to 90 days ahead. */
const MAX_DAYS_AHEAD = 90

const weekDays: { value: WeekDay; label: string }[] = [
  { value: 'mon', label: 'Mon' },
  { value: 'tue', label: 'Tue' },
  { value: 'wed', label: 'Wed' },
  { value: 'thu', label: 'Thu' },
  { value: 'fri', label: 'Fri' },
  { value: 'sat', label: 'Sat' },
  { value: 'sun', label: 'Sun' },
]

interface FormValues {
  trip_types: BulkTripsInput['trip_types']
  route_id: string
  bus_id: string
  driver_id: string
  from: string
  to: string
  weekdays: WeekDay[]
  notes?: string
  morning_start_time?: string
  morning_end_time?: string
  evening_start_time?: string
  evening_end_time?: string
}

/** Every date from..to (inclusive) that falls on one of the weekdays. */
function datesBetween(from?: string, to?: string, weekdays: string[] = []): string[] {
  if (!from || !to || to < from) return []
  const out: string[] = []
  for (let d = from; d <= to && out.length <= MAX_DAYS_AHEAD; d = addDays(d, 1)) {
    if (weekdays.includes(weekdayOf(d))) out.push(d)
  }
  return out
}

/**
 * Assigns one route + bus + driver to Morning Pickup and/or Evening Drop on many
 * dates at once (POST /trips/bulk). Dates whose slot is already taken are skipped
 * and listed; nothing already assigned is changed.
 */
export default function BulkAssignModal({
  schoolId,
  today,
  workingDays,
  routes,
  buses,
  drivers,
  initial,
  onClose,
  onDone,
}: {
  schoolId: string
  today: string
  workingDays: WeekDay[]
  routes: BusRoute[]
  buses: Bus[]
  drivers: Driver[]
  initial?: { route_id?: string; from?: string }
  onClose: () => void
  onDone: (firstDate: string | null) => void
}) {
  const { message, modal } = App.useApp()
  const [form] = Form.useForm<FormValues>()
  const [saving, setSaving] = useState(false)
  const [schedule, setSchedule] = useState<RouteSchedule | null>(null)
  const lastDay = addDays(today, MAX_DAYS_AHEAD)
  const tripTypes = Form.useWatch('trip_types', form) ?? []
  const routeId = Form.useWatch('route_id', form)
  const from = Form.useWatch('from', form)
  const to = Form.useWatch('to', form)
  const weekdays = Form.useWatch('weekdays', form)
  const dates = useMemo(() => datesBetween(from, to, weekdays), [from, to, weekdays])
  const total = dates.length * tripTypes.length
  const selectedRoute = useMemo(() => routes.find((r) => r.id === routeId), [routes, routeId])

  // The route decides which trip types are even checked by default — never
  // the other way round — but only once we know it actually HAS a usable
  // schedule for that type: a type the route supports but whose stops carry
  // no time at all is left unchecked (still offered below, with a warning,
  // for the admin to fill in manually — this is the one case "supports the
  // type" and "ready to auto-assign" diverge). Prefills each checked type's
  // window from the route's own stop times at the same time (never
  // invented); the admin can still edit either field afterwards.
  useEffect(() => {
    if (!routeId) return
    let cancelled = false
    routesApi.schedule(schoolId, routeId).then((s) => {
      if (cancelled) return
      setSchedule(s)
      const checked: BulkTripsInput['trip_types'] = []
      if (s.morning_pickup.supported && s.morning_pickup.start_time) checked.push('morning_pickup')
      if (s.evening_drop.supported && s.evening_drop.start_time) checked.push('evening_drop')
      form.setFieldsValue({
        trip_types: checked,
        morning_start_time: s.morning_pickup.start_time ?? undefined,
        morning_end_time: s.morning_pickup.end_time ?? undefined,
        evening_start_time: s.evening_drop.start_time ?? undefined,
        evening_end_time: s.evening_drop.end_time ?? undefined,
      })
    })
    return () => {
      cancelled = true
    }
  }, [schoolId, routeId, form])

  // Any active route with stops — the route itself now decides which trip
  // types apply (above), so the route list no longer needs to be narrowed
  // by whatever's currently checked.
  const usable = routes.filter((r) => r.stop_count > 0)

  async function save(v: FormValues) {
    setSaving(true)
    try {
      const res = await tripsApi.bulk(schoolId, {
        dates,
        trip_types: v.trip_types,
        route_id: v.route_id,
        bus_id: v.bus_id,
        driver_id: v.driver_id,
        notes: v.notes,
        morning_start_time: v.trip_types.includes('morning_pickup') ? v.morning_start_time : undefined,
        morning_end_time: v.trip_types.includes('morning_pickup') ? v.morning_end_time : undefined,
        evening_start_time: v.trip_types.includes('evening_drop') ? v.evening_start_time : undefined,
        evening_end_time: v.trip_types.includes('evening_drop') ? v.evening_end_time : undefined,
      })
      if (res.skipped.length === 0) {
        message.success(`${res.created} trip(s) assigned.`)
      } else {
        modal.info({
          title: `${res.created} trip(s) assigned, ${res.skipped.length} skipped`,
          width: 600,
          content: (
            <ul style={{ paddingLeft: 18, maxHeight: 360, overflow: 'auto' }}>
              {res.skipped.map((s, i) => (
                <li key={i}>
                  {formatDate(s.trip_date)} · {tripTypeLabels[s.trip_type]}: {s.reason}
                </li>
              ))}
            </ul>
          ),
        })
      }
      onDone(dates[0] ?? null)
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
      title="Bulk assign trips"
      okText={total ? `Assign ${total} trip(s)` : 'Assign'}
      okButtonProps={{ disabled: total === 0 }}
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      destroyOnHidden
      width={600}
    >
      <Typography.Paragraph type="secondary">
        The same route, bus and driver on every chosen day. Days where the route, bus or driver
        already has a trip are skipped and listed; existing trips are not changed.
      </Typography.Paragraph>
      <Form<FormValues>
        form={form}
        layout="vertical"
        onFinish={save}
        initialValues={{
          trip_types: ['morning_pickup', 'evening_drop'],
          route_id: initial?.route_id,
          from: initial?.from && initial.from > today ? initial.from : today,
          to: addDays(initial?.from && initial.from > today ? initial.from : today, 27),
          weekdays: workingDays.length ? workingDays : ['mon', 'tue', 'wed', 'thu', 'fri'],
        }}
      >
        <Form.Item name="route_id" label="Route" rules={[{ required: true }]}>
          <Select
            showSearch
            optionFilterProp="label"
            placeholder="Choose a route"
            notFoundContent="No active route has stops yet"
            options={usable.map((r) => ({ value: r.id, label: `${r.code} · ${r.name}` }))}
          />
        </Form.Item>
        <Form.Item
          name="trip_types"
          label="Trip types"
          rules={[{ required: true, message: 'Choose at least one' }]}
          extra={!selectedRoute ? 'Choose a route first — only the trip types it runs are offered.' : undefined}
        >
          <Checkbox.Group
            options={[
              { value: 'morning_pickup', label: tripTypeLabels.morning_pickup },
              { value: 'evening_drop', label: tripTypeLabels.evening_drop },
            ].filter(
              (o) =>
                !selectedRoute ||
                (o.value === 'morning_pickup' ? selectedRoute.supports_pickup : selectedRoute.supports_drop),
            )}
          />
        </Form.Item>
        <Flex gap="middle">
          <Form.Item name="bus_id" label="Bus" rules={[{ required: true }]} style={{ flex: 1 }}>
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="Choose a bus"
              options={buses.map((b) => ({
                value: b.id,
                label: `${b.vehicle_number} (${b.capacity} seats)`,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="driver_id"
            label="Driver"
            rules={[{ required: true }]}
            style={{ flex: 1 }}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="Choose a driver"
              options={drivers.map((d) => ({ value: d.id, label: `${d.name} · ${d.mobile}` }))}
            />
          </Form.Item>
        </Flex>
        {tripTypes.includes('morning_pickup') && (
          <Flex gap="middle">
            <Form.Item
              name="morning_start_time"
              label="Morning Pickup start"
              style={{ flex: 1 }}
              rules={[{ required: true, message: 'Give a start time.' }]}
            >
              <Input type="time" />
            </Form.Item>
            <Form.Item
              name="morning_end_time"
              label="Morning Pickup end"
              style={{ flex: 1 }}
              rules={[{ required: true, message: 'Give an end time.' }]}
            >
              <Input type="time" />
            </Form.Item>
          </Flex>
        )}
        {routeId && tripTypes.includes('morning_pickup') && schedule && !schedule.morning_pickup.start_time && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            title="This route's stops have no pickup time configured yet — enter a schedule manually."
          />
        )}
        {tripTypes.includes('evening_drop') && (
          <Flex gap="middle">
            <Form.Item
              name="evening_start_time"
              label="Evening Drop start"
              style={{ flex: 1 }}
              rules={[{ required: true, message: 'Give a start time.' }]}
            >
              <Input type="time" />
            </Form.Item>
            <Form.Item
              name="evening_end_time"
              label="Evening Drop end"
              style={{ flex: 1 }}
              rules={[{ required: true, message: 'Give an end time.' }]}
            >
              <Input type="time" />
            </Form.Item>
          </Flex>
        )}
        {routeId && tripTypes.includes('evening_drop') && schedule && !schedule.evening_drop.start_time && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            title="This route's stops have no drop time configured yet — enter a schedule manually."
          />
        )}
        <Flex gap="middle">
          <Form.Item name="from" label="From" rules={[{ required: true }]} style={{ flex: 1 }}>
            <Input type="date" min={today} max={lastDay} />
          </Form.Item>
          <Form.Item
            name="to"
            label="To"
            rules={[{ required: true }]}
            style={{ flex: 1 }}
            extra={`Up to ${formatDate(lastDay)} (${MAX_DAYS_AHEAD} days ahead)`}
          >
            <Input type="date" min={from || today} max={lastDay} />
          </Form.Item>
        </Flex>
        <Form.Item
          name="weekdays"
          label="On"
          rules={[{ required: true, message: 'Choose at least one day' }]}
          extra={
            workingDays.length
              ? `School working days: ${workingDays.join(', ')}`
              : 'Working days are not set for this school'
          }
        >
          <Checkbox.Group options={weekDays} />
        </Form.Item>
        <Form.Item name="notes" label="Notes (optional)">
          <Input.TextArea rows={2} maxLength={500} />
        </Form.Item>
      </Form>
      <Alert
        type={total ? 'info' : 'warning'}
        showIcon
        title={
          total
            ? `${dates.length} day(s) × ${tripTypes.length} trip type(s) = ${total} trip(s), ${formatDate(dates[0])} to ${formatDate(dates[dates.length - 1])}`
            : 'No days selected: check the dates and weekdays.'
        }
      />
    </Modal>
  )
}
