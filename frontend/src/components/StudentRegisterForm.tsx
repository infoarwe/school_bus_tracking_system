import { useEffect, useState } from 'react'
import {
  Alert,
  App,
  Col,
  Descriptions,
  Divider,
  Flex,
  Form,
  Input,
  Modal,
  Radio,
  Row,
  Select,
  Spin,
  Tag,
  Typography,
} from 'antd'
import { CheckCircleFilled, UserAddOutlined, UserOutlined } from '@ant-design/icons'
import { parentsApi, studentsApi } from '../services/people'
import { routesApi } from '../services/transport'
import type {
  BusRoute,
  Parent,
  Relationship,
  Stop,
  Student,
  TransportStatus,
} from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'
import { formatMobile, mobileDigits } from '../utils/mobile'

interface Values {
  admission_no: string
  name: string
  class?: string
  section?: string
  parent_mobile?: string
  parent_name?: string
  relationship: Relationship
  transport_status: TransportStatus
  route_id?: string
  pickup_stop_id?: string
  drop_stop_id?: string
}

const RELATIONSHIPS = [
  { value: 'father', label: 'Father' },
  { value: 'mother', label: 'Mother' },
  { value: 'guardian', label: 'Guardian' },
]

type Lookup =
  | { state: 'idle' }
  | { state: 'searching' }
  | { state: 'found'; parent: Parent }
  | { state: 'new' }
  | { state: 'failed'; message: string }

/**
 * Register a student in one form: the student, their parent (an existing parent is
 * found by mobile number and linked, so one parent can have several children), and
 * their route and stops. Saves in steps; if a later step fails, the earlier ones are
 * kept and Save retries only what is left.
 */
export default function StudentRegisterForm({
  schoolId,
  onClose,
  onSaved,
}: {
  schoolId: string
  onClose: () => void
  onSaved: (s: Student) => void
}) {
  const [form] = Form.useForm<Values>()
  const { message } = App.useApp()
  const [saving, setSaving] = useState(false)
  // What has been saved so far (a retry continues from here).
  const [created, setCreated] = useState<Student | null>(null)
  const [parentDone, setParentDone] = useState(false)
  const [stepError, setStepError] = useState<string | null>(null)

  const [routes, setRoutes] = useState<BusRoute[] | null>(null)

  const values = Form.useWatch([], form) as Partial<Values> | undefined
  const mobile = Form.useWatch('parent_mobile', form)
  const routeId = Form.useWatch('route_id', form)
  const usesTransport = Form.useWatch('transport_status', form) !== 'not_using'

  // Results are kept with the input they belong to, so stale answers are ignored and
  // nothing is reset synchronously when the input changes.
  const digits = mobileDigits(mobile)
  const [lookupResult, setLookupResult] = useState<{ digits: string; lookup: Lookup } | null>(null)
  const lookup: Lookup = !digits
    ? { state: 'idle' }
    : lookupResult?.digits === digits
      ? lookupResult.lookup
      : { state: 'searching' }
  const [loadedRoute, setLoadedRoute] = useState<BusRoute | null>(null)
  const route = routeId && loadedRoute?.id === routeId ? loadedRoute : null
  const stops: Stop[] = route?.stops ?? []
  const stopsLoading = !!routeId && !route

  useEffect(() => {
    routesApi
      .list(schoolId, { status: 'active', page_size: 100 })
      .then((r) => setRoutes(r.data))
      .catch(() => setRoutes([]))
  }, [schoolId])

  // Look the parent up by mobile number as soon as it is complete.
  useEffect(() => {
    if (!digits) return
    let cancelled = false
    const t = window.setTimeout(() => {
      parentsApi
        .list(schoolId, { q: digits, page_size: 5 })
        .then((res) => {
          const match = res.data.find((p) => mobileDigits(p.mobile) === digits)
          if (!cancelled) {
            setLookupResult({
              digits,
              lookup: match ? { state: 'found', parent: match } : { state: 'new' },
            })
          }
        })
        .catch((e: unknown) => {
          if (!cancelled) {
            setLookupResult({ digits, lookup: { state: 'failed', message: errorMessage(e) } })
          }
        })
    }, 350)
    return () => {
      cancelled = true
      window.clearTimeout(t)
    }
  }, [digits, schoolId])

  // Only stops of the chosen route can be picked (rule 5); drop stale picks on change.
  useEffect(() => {
    if (!routeId) return
    let cancelled = false
    routesApi
      .get(schoolId, routeId)
      .then((r) => {
        if (cancelled) return
        setLoadedRoute(r)
        const ids = new Set((r.stops ?? []).map((s) => s.id))
        const { pickup_stop_id, drop_stop_id } = form.getFieldsValue()
        form.setFieldsValue({
          pickup_stop_id: pickup_stop_id && ids.has(pickup_stop_id) ? pickup_stop_id : undefined,
          drop_stop_id: drop_stop_id && ids.has(drop_stop_id) ? drop_stop_id : undefined,
        })
      })
      .catch((e: unknown) => !cancelled && message.error(errorMessage(e)))
    return () => {
      cancelled = true
    }
  }, [routeId, schoolId, form, message])

  async function save(v: Values) {
    // Never guess: creating a parent before the lookup finishes could duplicate one.
    if (mobileDigits(v.parent_mobile) && !parentDone) {
      if (lookup.state === 'searching') {
        message.info('Still checking the mobile number. Try again in a moment.')
        return
      }
      if (lookup.state === 'failed') {
        message.error(
          'Could not check whether this parent exists. Check the connection and try again.',
        )
        return
      }
    }
    setSaving(true)
    setStepError(null)
    let student = created
    try {
      // 1. The student.
      if (!student) {
        student = await studentsApi.create(schoolId, {
          admission_no: v.admission_no.trim(),
          name: v.name.trim(),
          class: (v.class ?? '').trim(),
          section: (v.section ?? '').trim(),
          transport_status: v.transport_status,
        })
        setCreated(student)
      }
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
      setSaving(false)
      return
    }

    // 2. The parent: link to the existing account, or create one.
    const digits = mobileDigits(v.parent_mobile)
    if (digits && !parentDone) {
      try {
        if (lookup.state === 'found') {
          const p = lookup.parent
          await parentsApi.update(schoolId, p.id, {
            name: p.name,
            mobile: p.mobile,
            alternate_mobile: p.alternate_mobile,
            email: p.email,
            address: p.address,
            children: [
              ...p.children.map((c) => ({
                student_id: c.student_id,
                relationship: c.relationship,
              })),
              { student_id: student.id, relationship: v.relationship },
            ],
          })
        } else {
          await parentsApi.create(schoolId, {
            name: (v.parent_name ?? '').trim(),
            mobile: digits,
            alternate_mobile: '',
            email: '',
            address: '',
            children: [{ student_id: student.id, relationship: v.relationship }],
          })
        }
        setParentDone(true)
      } catch (e) {
        const msg = applyApiErrors(form, e, { name: 'parent_name', mobile: 'parent_mobile' })
        setStepError(
          `${student.name} was saved, but the parent was not${msg ? `: ${msg}` : '. See the highlighted field.'} ` +
            'Fix it and press Save again, or close and link the parent later from the Parents page.',
        )
        setSaving(false)
        return
      }
    }

    // 3. Route and stops.
    if (v.transport_status === 'uses_transport' && v.route_id && route) {
      try {
        await studentsApi.assign(schoolId, student.id, {
          route_id: v.route_id,
          pickup_stop_id: route.supports_pickup ? (v.pickup_stop_id ?? null) : null,
          drop_stop_id: route.supports_drop ? (v.drop_stop_id ?? null) : null,
        })
      } catch (e) {
        const msg = applyApiErrors(form, e)
        setStepError(
          `${student.name} was saved${digits ? ' and linked to the parent' : ''}, but the route was not` +
            `${msg ? `: ${msg}` : '. See the highlighted field.'} Fix it and press Save again, or ` +
            'close and use "Route & stop" later.',
        )
        setSaving(false)
        return
      }
    }

    message.success(`${student.name} is registered.`)
    setSaving(false)
    onSaved(student)
  }

  const stopOptions = stops.map((s) => ({
    value: s.id,
    label: `${s.sequence}. ${s.name}${s.landmark ? ` · ${s.landmark}` : ''}`,
  }))
  const stopName = (id?: string) => stops.find((s) => s.id === id)?.name

  return (
    <Modal
      open
      title={created ? `Finish registering ${created.name}` : 'Register a student'}
      okText={created ? 'Save the rest' : 'Register student'}
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={() => (created ? onSaved(created) : onClose())}
      cancelText={created ? 'Close (finish later)' : 'Cancel'}
      width={{ xs: '100%', md: 760 }}
      mask={{ closable: false }}
      destroyOnHidden
    >
      {stepError && (
        <Alert type="warning" showIcon title={stepError} style={{ marginBottom: 16 }} />
      )}
      <Form
        form={form}
        layout="vertical"
        requiredMark="optional"
        initialValues={{ transport_status: 'uses_transport', relationship: 'father' }}
        onFinish={save}
        scrollToFirstError
        onValuesChange={(changed: Partial<Values>) => {
          // Most students get on and off at the same stop.
          if (changed.pickup_stop_id && !form.getFieldValue('drop_stop_id')) {
            form.setFieldValue('drop_stop_id', changed.pickup_stop_id)
          }
        }}
      >
        <div className="form-section-title">1 · Student</div>
        <Row gutter={12}>
          <Col xs={24} sm={14}>
            <Form.Item
              name="name"
              label="Student name"
              rules={[
                { required: true, whitespace: true, message: 'Enter the student name.' },
                { max: 200 },
              ]}
            >
              <Input autoFocus disabled={!!created} />
            </Form.Item>
          </Col>
          <Col xs={24} sm={10}>
            <Form.Item
              name="admission_no"
              label="Admission no."
              rules={[
                { required: true, whitespace: true, message: 'Enter the admission number.' },
                { max: 30 },
              ]}
            >
              <Input style={{ textTransform: 'uppercase' }} disabled={!!created} />
            </Form.Item>
          </Col>
          <Col xs={12} sm={6}>
            <Form.Item name="class" label="Class">
              <Input placeholder="5" disabled={!!created} />
            </Form.Item>
          </Col>
          <Col xs={12} sm={6}>
            <Form.Item name="section" label="Section">
              <Input placeholder="A" style={{ textTransform: 'uppercase' }} disabled={!!created} />
            </Form.Item>
          </Col>
        </Row>

        <Divider style={{ margin: '4px 0 12px' }} />
        <div className="form-section-title">2 · Parent or guardian</div>
        <Row gutter={12}>
          <Col xs={24} sm={12}>
            <Form.Item
              name="parent_mobile"
              label="Mobile number"
              extra="The parent logs in to the Parent app with this number."
              rules={[
                {
                  validator: (_, v: string | undefined) =>
                    !v || mobileDigits(v)
                      ? Promise.resolve()
                      : Promise.reject(new Error('Enter a valid 10-digit mobile number.')),
                },
              ]}
            >
              <Input
                inputMode="tel"
                placeholder="98765 43210"
                prefix="+91"
                allowClear
                disabled={parentDone}
              />
            </Form.Item>
          </Col>
          <Col xs={24} sm={12}>
            <Form.Item name="relationship" label="Relationship to the student">
              <Radio.Group
                options={RELATIONSHIPS}
                optionType="button"
                buttonStyle="solid"
                disabled={parentDone}
              />
            </Form.Item>
          </Col>
        </Row>
        <ParentLookup lookup={lookup} done={parentDone} />
        {lookup.state === 'new' && !parentDone && (
          <Form.Item
            name="parent_name"
            label="Parent name"
            rules={[
              { required: true, whitespace: true, message: 'Enter the parent name.' },
              { max: 200 },
            ]}
          >
            <Input placeholder="New parent account" />
          </Form.Item>
        )}

        <Divider style={{ margin: '4px 0 12px' }} />
        <div className="form-section-title">3 · School transport</div>
        <Form.Item name="transport_status">
          <Radio.Group
            disabled={!!created}
            options={[
              { value: 'uses_transport', label: 'Uses the school bus' },
              { value: 'not_using', label: 'Does not use school transport' },
            ]}
          />
        </Form.Item>
        {usesTransport && (
          <>
            <Form.Item
              name="route_id"
              label="Route"
              extra="Optional now: you can assign the route later with “Route & stop”."
            >
              <Select
                allowClear
                showSearch={{ optionFilterProp: 'label' }}
                loading={routes === null}
                placeholder="Choose a route"
                options={(routes ?? []).map((r) => ({
                  value: r.id,
                  label: `${r.code} · ${r.name}${r.stop_count === 0 ? ' (no stops yet)' : ''}`,
                  disabled: r.stop_count === 0,
                }))}
              />
            </Form.Item>
            {routeId && (
              <Spin spinning={stopsLoading}>
                <Row gutter={12}>
                  {route?.supports_pickup && (
                    <Col xs={24} sm={12}>
                      <Form.Item
                        name="pickup_stop_id"
                        label="Pickup stop (Morning Pickup)"
                        rules={[{ required: true, message: 'Choose the pickup stop.' }]}
                      >
                        <Select
                          showSearch={{ optionFilterProp: 'label' }}
                          options={stopOptions}
                          placeholder="Stops of this route"
                        />
                      </Form.Item>
                    </Col>
                  )}
                  {route?.supports_drop && (
                    <Col xs={24} sm={12}>
                      <Form.Item
                        name="drop_stop_id"
                        label="Drop stop (Evening Drop)"
                        rules={[{ required: true, message: 'Choose the drop stop.' }]}
                      >
                        <Select
                          showSearch={{ optionFilterProp: 'label' }}
                          options={stopOptions}
                          placeholder="Stops of this route"
                        />
                      </Form.Item>
                    </Col>
                  )}
                </Row>
              </Spin>
            )}
          </>
        )}

        <Divider style={{ margin: '4px 0 12px' }} />
        <div className="form-section-title">4 · Review</div>
        <Descriptions size="small" column={1} bordered>
          <Descriptions.Item label="Student">
            {values?.name || '—'}
            {values?.admission_no && ` · ${values.admission_no.toUpperCase()}`}
            {values?.class &&
              ` · Class ${values.class}${values.section ? `-${values.section.toUpperCase()}` : ''}`}
          </Descriptions.Item>
          <Descriptions.Item label="Parent">
            {lookup.state === 'found'
              ? `Link to ${lookup.parent.name} (${formatMobile(lookup.parent.mobile)}) as ${values?.relationship}`
              : lookup.state === 'new'
                ? `New parent ${values?.parent_name || '…'} (${formatMobile(values?.parent_mobile)}) as ${values?.relationship}`
                : 'None (link one later from the Parents page)'}
          </Descriptions.Item>
          <Descriptions.Item label="Transport">
            {!usesTransport
              ? 'Does not use school transport'
              : route && routeId
                ? [
                    `${route.code} · ${route.name}`,
                    route.supports_pickup && `pickup at ${stopName(values?.pickup_stop_id) ?? '…'}`,
                    route.supports_drop && `drop at ${stopName(values?.drop_stop_id) ?? '…'}`,
                  ]
                    .filter(Boolean)
                    .join(', ')
                : 'Route not chosen yet (assign later)'}
          </Descriptions.Item>
        </Descriptions>
      </Form>
    </Modal>
  )
}

function ParentLookup({ lookup, done }: { lookup: Lookup; done: boolean }) {
  if (done) {
    return (
      <Alert
        type="success"
        showIcon
        icon={<CheckCircleFilled />}
        title="Parent linked."
        style={{ marginBottom: 16 }}
      />
    )
  }
  switch (lookup.state) {
    case 'searching':
      return (
        <Typography.Paragraph type="secondary">
          <Spin size="small" /> Looking for an existing parent with this number…
        </Typography.Paragraph>
      )
    case 'found':
      return (
        <Alert
          type="info"
          showIcon
          icon={<UserOutlined />}
          style={{ marginBottom: 16 }}
          title={`Existing parent: ${lookup.parent.name}`}
          description={
            <Flex vertical gap={4}>
              <span>The student will be added to this parent's account (no duplicate login).</span>
              {lookup.parent.children.length > 0 && (
                <span>
                  Already linked:{' '}
                  {lookup.parent.children.map((c) => (
                    <Tag key={c.student_id}>
                      {c.name}
                      {c.class && ` · ${c.class}${c.section ? `-${c.section}` : ''}`}
                    </Tag>
                  ))}
                </span>
              )}
            </Flex>
          }
        />
      )
    case 'new':
      return (
        <Typography.Paragraph type="secondary">
          <UserAddOutlined /> No parent with this number yet: a new parent account will be created.
        </Typography.Paragraph>
      )
    case 'failed':
      return (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          title={`Could not check for an existing parent: ${lookup.message}`}
        />
      )
    default:
      return (
        <Typography.Paragraph type="secondary">
          Optional. Leave empty to link a parent later.
        </Typography.Paragraph>
      )
  }
}
