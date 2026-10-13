import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { App, Button, Checkbox, Flex, Form, Modal, Select, Space, Table, Tag } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import LoadingOrError from '../components/LoadingOrError'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import { useLoad } from '../hooks/useLoad'
import { driverAssignmentsApi } from '../services/driverAssignments'
import { busesApi, driversApi, routesApi } from '../services/transport'
import type {
  Bus,
  BusRoute,
  Driver,
  DriverRouteAssignment,
  DriverRouteAssignmentInput,
} from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

type TripType = DriverRouteAssignmentInput['trip_type']

const tripTypeLabel: Record<TripType, string> = {
  morning_pickup: 'Morning Pickup',
  evening_drop: 'Evening Drop',
}
const tripTypeColor: Record<TripType, string> = { morning_pickup: 'blue', evening_drop: 'purple' }

export default function DriverAssignmentsPage() {
  return <RequireSchool>{(schoolId) => <DriverAssignments schoolId={schoolId} />}</RequireSchool>
}

interface Masters {
  routes: BusRoute[]
  buses: Bus[]
  drivers: Driver[]
}

function DriverAssignments({ schoolId }: { schoolId: string }) {
  const { message, modal } = App.useApp()
  // ?driver_id= filters to one driver (link from the Drivers page).
  const [searchParams] = useSearchParams()
  const driverId = searchParams.get('driver_id') ?? undefined

  const [masters, setMasters] = useState<Masters>({ routes: [], buses: [], drivers: [] })
  useEffect(() => {
    Promise.all([
      routesApi.list(schoolId, { status: 'active', page_size: 100 }),
      busesApi.list(schoolId, { status: 'active', page_size: 100 }),
      driversApi.list(schoolId, { status: 'active', page_size: 100 }),
    ])
      .then(([r, b, d]) => setMasters({ routes: r.data, buses: b.data, drivers: d.data }))
      .catch((e: unknown) => message.error(errorMessage(e)))
  }, [schoolId, message])

  const loadAssignments = useCallback(
    () => driverAssignmentsApi.list(schoolId, driverId),
    [schoolId, driverId],
  )
  const { data: assignments, setData, error, retry } = useLoad(loadAssignments)
  const [editing, setEditing] = useState<DriverRouteAssignment | 'new' | null>(null)

  const driverName = driverId ? masters.drivers.find((d) => d.id === driverId)?.name : undefined

  function deactivate(a: DriverRouteAssignment) {
    modal.confirm({
      title: `Deactivate this ${tripTypeLabel[a.trip_type]} assignment?`,
      content: `${a.driver.name} will no longer get a ${tripTypeLabel[a.trip_type]} trip on route ${a.route.code} starting tomorrow. Already-created trips are not affected.`,
      okButtonProps: { danger: true },
      okText: 'Deactivate',
      onOk: async () => {
        try {
          await driverAssignmentsApi.deactivate(schoolId, a.id)
          message.success('Assignment deactivated.')
          setData((prev) => (prev ?? []).filter((x) => x.id !== a.id))
        } catch (e) {
          message.error(errorMessage(e))
        }
      },
    })
  }

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Driver Assignments"
        subtitle={
          driverName
            ? `Permanent Morning/Evening routes for ${driverName}`
            : 'Permanent Morning/Evening route assignments, set once until changed'
        }
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditing('new')}>
            Add assignment
          </Button>
        }
      />
      {!assignments ? (
        <LoadingOrError error={error} onRetry={retry} />
      ) : (
        <Table<DriverRouteAssignment>
          rowKey="id"
          dataSource={assignments}
          locale={{ emptyText: 'No permanent assignments yet.' }}
          columns={[
            {
              title: 'Trip type',
              dataIndex: 'trip_type',
              render: (t: TripType) => <Tag color={tripTypeColor[t]}>{tripTypeLabel[t]}</Tag>,
            },
            {
              title: 'Route',
              render: (_, a) => `${a.route.code} — ${a.route.name}`,
            },
            { title: 'Driver', render: (_, a) => a.driver.name },
            { title: 'Bus', render: (_, a) => a.bus.vehicle_number },
            {
              title: 'Assigned since',
              dataIndex: 'created_at',
              render: (v: string) => new Date(v).toLocaleDateString(),
            },
            {
              title: 'Actions',
              width: 180,
              render: (_, a) => (
                <Space size="small">
                  <Button size="small" onClick={() => setEditing(a)}>
                    Change
                  </Button>
                  <Button size="small" danger onClick={() => deactivate(a)}>
                    Deactivate
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      )}
      {editing && (
        <AssignmentForm
          schoolId={schoolId}
          masters={masters}
          assignment={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={(saved) => {
            setEditing(null)
            setData((prev) => {
              const rest = (prev ?? []).filter((x) => !saved.some((s) => s.id === x.id))
              // Replacing an existing assignment (update) removes the old row too.
              const oldID = editing !== 'new' && editing ? editing.id : null
              return [...rest.filter((x) => x.id !== oldID), ...saved]
            })
          }}
        />
      )}
    </Flex>
  )
}

function AssignmentForm({
  schoolId,
  masters,
  assignment,
  onClose,
  onSaved,
}: {
  schoolId: string
  masters: Masters
  assignment: DriverRouteAssignment | null
  onClose: () => void
  onSaved: (saved: DriverRouteAssignment[]) => void
}) {
  const { message } = App.useApp()
  const [form] = Form.useForm<
    DriverRouteAssignmentInput & { also_evening?: boolean; evening_route_id?: string }
  >()
  const [saving, setSaving] = useState(false)
  const tripType = Form.useWatch('trip_type', form) ?? assignment?.trip_type ?? 'morning_pickup'
  const alsoEvening = Form.useWatch('also_evening', form)

  const routesFor = (t: TripType) =>
    masters.routes.filter((r) => (t === 'morning_pickup' ? r.supports_pickup : r.supports_drop))

  async function save(
    v: DriverRouteAssignmentInput & { also_evening?: boolean; evening_route_id?: string },
  ) {
    setSaving(true)
    try {
      const saved: DriverRouteAssignment[] = []
      if (assignment) {
        saved.push(
          await driverAssignmentsApi.update(schoolId, assignment.id, {
            route_id: v.route_id,
            trip_type: v.trip_type,
            driver_id: v.driver_id,
            bus_id: v.bus_id,
          }),
        )
      } else {
        saved.push(
          await driverAssignmentsApi.create(schoolId, {
            route_id: v.route_id,
            trip_type: v.trip_type,
            driver_id: v.driver_id,
            bus_id: v.bus_id,
          }),
        )
        if (v.also_evening && v.evening_route_id) {
          try {
            saved.push(
              await driverAssignmentsApi.create(schoolId, {
                route_id: v.evening_route_id,
                trip_type: 'evening_drop',
                driver_id: v.driver_id,
                bus_id: v.bus_id,
              }),
            )
          } catch (e) {
            // The Morning assignment above already succeeded; report the Evening one
            // separately rather than losing track of what was actually saved.
            message.error(
              'Morning assignment saved, but the Evening one failed: ' + errorMessage(e),
            )
          }
        }
      }
      message.success(assignment ? 'Assignment changed.' : 'Assignment created.')
      onSaved(saved)
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
      title={assignment ? 'Change assignment' : 'Add permanent assignment'}
      okText="Save"
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      destroyOnHidden
    >
      <Form
        form={form}
        layout="vertical"
        requiredMark="optional"
        initialValues={
          assignment
            ? {
                route_id: assignment.route.id,
                trip_type: assignment.trip_type,
                driver_id: assignment.driver.id,
                bus_id: assignment.bus.id,
              }
            : { trip_type: 'morning_pickup' }
        }
        onFinish={save}
      >
        <Form.Item name="trip_type" label="Trip type" rules={[{ required: true }]}>
          <Select
            disabled={!!assignment}
            options={[
              { value: 'morning_pickup', label: 'Morning Pickup' },
              { value: 'evening_drop', label: 'Evening Drop' },
            ]}
          />
        </Form.Item>
        <Form.Item
          name="route_id"
          label={tripType === 'morning_pickup' ? 'Morning route' : 'Evening route'}
          rules={[{ required: true }]}
        >
          <Select
            placeholder="Select a route"
            options={routesFor(tripType).map((r) => ({
              value: r.id,
              label: `${r.code} — ${r.name}`,
            }))}
          />
        </Form.Item>
        <Form.Item name="driver_id" label="Driver" rules={[{ required: true }]}>
          <Select
            placeholder="Select a driver"
            options={masters.drivers.map((d) => ({
              value: d.id,
              label: `${d.name} (${d.mobile})`,
            }))}
          />
        </Form.Item>
        <Form.Item name="bus_id" label="Bus" rules={[{ required: true }]}>
          <Select
            placeholder="Select a bus"
            options={masters.buses.map((b) => ({ value: b.id, label: b.vehicle_number }))}
          />
        </Form.Item>
        {!assignment && tripType === 'morning_pickup' && (
          <>
            <Form.Item name="also_evening" valuePropName="checked">
              <Checkbox>Also assign the Evening route to the same driver and bus</Checkbox>
            </Form.Item>
            {alsoEvening && (
              <Form.Item name="evening_route_id" label="Evening route" rules={[{ required: true }]}>
                <Select
                  placeholder="Select the evening route"
                  options={routesFor('evening_drop').map((r) => ({
                    value: r.id,
                    label: `${r.code} — ${r.name}`,
                  }))}
                />
              </Form.Item>
            )}
          </>
        )}
      </Form>
    </Modal>
  )
}
