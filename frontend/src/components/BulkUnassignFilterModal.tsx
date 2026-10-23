import { useEffect, useRef, useState } from 'react'
import {
  Alert,
  App,
  Checkbox,
  Flex,
  Form,
  Input,
  Modal,
  Radio,
  Select,
  Spin,
  Typography,
} from 'antd'
import { tripsApi } from '../services/trips'
import {
  tripTypeLabels,
  type Bus,
  type BulkUnassignFilter,
  type BulkUnassignPreviewResult,
  type BusRoute,
  type Driver,
  type TripType,
} from '../services/types'
import { formatDate } from '../utils/dates'
import { errorMessage } from '../utils/formErrors'

type DateMode = 'auto' | 'all' | 'range'

interface FormValues {
  mode: DateMode
  from?: string
  to?: string
  route_id?: string
  bus_id?: string
  driver_id?: string
  trip_type?: TripType
}

// A previewed scope older than this is treated as stale at confirm time: the
// admin is shown a refreshed count instead of acting on a number that might
// no longer be true (item 5 — never let the executed scope silently drift
// from what was actually confirmed).
const STALE_PREVIEW_MS = 20_000

/**
 * Bulk Unassign across dates (Trip Planner) — the date-range/All Dates
 * counterpart to Daily Trips' single-day row-selection Bulk Unassign. Never
 * silently limited to one day or to a manually-typed date range: picking a
 * route, driver or bus alone (the "Automatic" mode, the default) is enough —
 * its applicable assignment period is auto-detected server-side from the
 * matching trips themselves and shown before the confirm button is even
 * enabled. A custom date range and All Dates remain available as explicit,
 * optional alternatives.
 */
export default function BulkUnassignFilterModal({
  schoolId,
  routes,
  buses,
  drivers,
  onClose,
  onDone,
}: {
  schoolId: string
  routes: BusRoute[]
  buses: Bus[]
  drivers: Driver[]
  onClose: () => void
  onDone: () => void
}) {
  const { message, modal } = App.useApp()
  const [form] = Form.useForm<FormValues>()
  const mode = Form.useWatch('mode', form) ?? 'auto'
  const from = Form.useWatch('from', form)
  const to = Form.useWatch('to', form)
  const routeId = Form.useWatch('route_id', form)
  const busId = Form.useWatch('bus_id', form)
  const driverId = Form.useWatch('driver_id', form)
  const tripType = Form.useWatch('trip_type', form)
  const hasEntityFilter = !!(routeId || busId || driverId || tripType)

  // acknowledgedForScopeKey holds the scope the admin last ticked "I
  // understand" for (set only from the checkbox's own onChange, a plain
  // event handler — never from an effect). "Acknowledged" is then derived by
  // comparing it to the CURRENT scope below, so changing any filter or the
  // date range automatically un-acknowledges without any reset code at all —
  // a stale confirmation from a smaller/different scope can never silently
  // carry over to a new, possibly larger one.
  const [acknowledgedForScopeKey, setAcknowledgedForScopeKey] = useState<string | null>(null)
  // previewFor holds the scope its result belongs to, so a result from a
  // scope that's since changed is simply never shown (derived below) instead
  // of needing to be actively cleared.
  const [previewFor, setPreviewFor] = useState<{
    scopeKey: string
    loading: boolean
    data: BulkUnassignPreviewResult | null
    error: string | null
    fetchedAt: number
  } | null>(null)
  const [busy, setBusy] = useState(false)
  // Guards against an out-of-order network response: only the response for
  // the most recently issued request is ever applied to state.
  const latestRequestID = useRef(0)

  const scopeKey = JSON.stringify({ mode, from, to, routeId, busId, driverId, tripType })
  const acknowledged = acknowledgedForScopeKey === scopeKey
  const preview = previewFor?.scopeKey === scopeKey ? previewFor.data : null
  const previewLoading = previewFor?.scopeKey === scopeKey && previewFor.loading
  const previewError = previewFor?.scopeKey === scopeKey ? previewFor.error : null

  const filter = (): BulkUnassignFilter => ({
    all_dates: mode === 'all' ? true : undefined,
    from: mode === 'range' ? from : undefined,
    to: mode === 'range' ? to : undefined,
    route_id: routeId || undefined,
    bus_id: busId || undefined,
    driver_id: driverId || undefined,
    trip_type: tripType || undefined,
  })

  // A scope is ready to preview once it says enough to resolve without
  // defaulting to "every trip": Automatic needs a route/bus/driver/trip type,
  // All Dates needs nothing more, a custom range needs both ends.
  const ready =
    mode === 'all' ? true : mode === 'auto' ? hasEntityFilter : !!from && !!to

  function runPreview(key: string) {
    const requestID = ++latestRequestID.current
    setPreviewFor({ scopeKey: key, loading: true, data: null, error: null, fetchedAt: Date.now() })
    tripsApi
      .bulkUnassignPreview(schoolId, filter())
      .then((res) => {
        if (requestID !== latestRequestID.current) return // a newer request has since superseded this one
        setPreviewFor({ scopeKey: key, loading: false, data: res, error: null, fetchedAt: Date.now() })
      })
      .catch((e: unknown) => {
        if (requestID !== latestRequestID.current) return
        setPreviewFor({
          scopeKey: key,
          loading: false,
          data: null,
          error: errorMessage(e),
          fetchedAt: Date.now(),
        })
      })
  }

  // Debounced live preview: re-fetch the exact matching count and
  // auto-detected date span whenever the scope changes — the confirm step
  // never becomes available without this having run successfully at least
  // once. Every state update happens inside a callback (the timer, or a
  // promise settling), never synchronously in the effect body itself.
  useEffect(() => {
    const key = scopeKey
    const handle = setTimeout(() => {
      if (!ready) {
        setPreviewFor({ scopeKey: key, loading: false, data: null, error: null, fetchedAt: Date.now() })
        return
      }
      runPreview(key)
    }, 350)
    return () => clearTimeout(handle)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [schoolId, scopeKey, ready])

  async function submit() {
    // If the preview this confirm would act on is old enough that matching
    // trips may well have changed since, refresh it instead of trusting a
    // possibly-stale number — the admin reviews the new count and confirms
    // again, rather than the operation silently running against a scope
    // that's no longer accurate.
    if (previewFor && previewFor.scopeKey === scopeKey && Date.now() - previewFor.fetchedAt > STALE_PREVIEW_MS) {
      message.info('Refreshing the matching trips before unassigning — please review and confirm again.')
      runPreview(scopeKey)
      return
    }
    setBusy(true)
    try {
      const res = await tripsApi.bulkUnassignFilter(schoolId, filter())
      const ok = res.results.filter((r) => r.status === 'unassigned').length
      const failed = res.results.filter((r) => r.status === 'skipped')
      if (failed.length === 0) {
        message.success(`${ok} trip(s) unassigned.`)
      } else {
        modal.info({
          title: `${ok} unassigned, ${failed.length} could not be`,
          width: 560,
          content: (
            <ul style={{ paddingLeft: 18, maxHeight: 320, overflow: 'auto' }}>
              {failed.map((f) => (
                <li key={f.trip_id}>
                  {f.trip_id}: {f.reason}
                </li>
              ))}
            </ul>
          ),
        })
      }
      onDone()
    } catch (e) {
      message.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  const canConfirm =
    !previewLoading &&
    !previewError &&
    !!preview &&
    preview.count > 0 &&
    !preview.over_limit &&
    (mode === 'all' ? acknowledged : true)

  return (
    <Modal
      open
      title="Bulk unassign trips"
      okText={preview ? `Unassign ${preview.eligible} trip(s)` : 'Unassign'}
      okButtonProps={{ danger: true, disabled: !canConfirm }}
      confirmLoading={busy}
      onOk={() => void submit()}
      onCancel={onClose}
      width={560}
    >
      <Typography.Paragraph type="secondary">
        Clears bus and driver only — route, schedule, stops and students are untouched. Only
        scheduled/confirmed trips are affected; a trip already started, completed or cancelled is
        reported and skipped, never silently dropped.
      </Typography.Paragraph>
      <Form<FormValues> form={form} layout="vertical" initialValues={{ mode: 'auto' }}>
        <Form.Item name="mode" label="Scope">
          <Radio.Group
            options={[
              { label: 'Automatically detected period', value: 'auto' },
              { label: 'All dates', value: 'all' },
              { label: 'Custom date range', value: 'range' },
            ]}
          />
        </Form.Item>
        {mode === 'auto' && !hasEntityFilter && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 16 }}
            title="Select a route, driver or bus below — its applicable period is detected automatically from its own trips. Choose All Dates instead if you want every trip regardless of route/driver/bus."
          />
        )}
        {mode === 'range' && (
          <Flex gap="middle">
            <Form.Item name="from" label="From" rules={[{ required: true }]} style={{ flex: 1 }}>
              <Input type="date" />
            </Form.Item>
            <Form.Item name="to" label="To" rules={[{ required: true }]} style={{ flex: 1 }}>
              <Input type="date" min={from} />
            </Form.Item>
          </Flex>
        )}
        {mode === 'all' && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            title="This targets every eligible date — not just what's on screen. Use the filters below to narrow it, or acknowledge below to proceed with all of them."
          />
        )}
        <Flex gap="middle">
          <Form.Item name="route_id" label="Route (optional)" style={{ flex: 1 }}>
            <Select
              allowClear
              showSearch
              optionFilterProp="label"
              placeholder="Any route"
              options={routes.map((r) => ({ value: r.id, label: `${r.code} · ${r.name}` }))}
            />
          </Form.Item>
          <Form.Item name="trip_type" label="Trip type (optional)" style={{ flex: 1 }}>
            <Select
              allowClear
              placeholder="Any type"
              options={[
                { value: 'morning_pickup', label: tripTypeLabels.morning_pickup },
                { value: 'evening_drop', label: tripTypeLabels.evening_drop },
                { value: 'special_trip', label: tripTypeLabels.special_trip },
              ]}
            />
          </Form.Item>
        </Flex>
        <Flex gap="middle">
          <Form.Item name="bus_id" label="Bus (optional)" style={{ flex: 1 }}>
            <Select
              allowClear
              showSearch
              optionFilterProp="label"
              placeholder="Any bus"
              options={buses.map((b) => ({ value: b.id, label: b.vehicle_number }))}
            />
          </Form.Item>
          <Form.Item name="driver_id" label="Driver (optional)" style={{ flex: 1 }}>
            <Select
              allowClear
              showSearch
              optionFilterProp="label"
              placeholder="Any driver"
              options={drivers.map((d) => ({ value: d.id, label: `${d.name} · ${d.mobile}` }))}
            />
          </Form.Item>
        </Flex>
      </Form>

      {previewLoading ? (
        <Flex gap="small" align="center">
          <Spin size="small" /> <Typography.Text type="secondary">Checking matching trips…</Typography.Text>
        </Flex>
      ) : previewError ? (
        <Alert type="error" showIcon title={previewError} />
      ) : preview ? (
        preview.count === 0 ? (
          <Alert type="warning" showIcon title="No trips match this scope." />
        ) : preview.over_limit ? (
          <Alert
            type="error"
            showIcon
            title={`${preview.count} trips match — more than the ${preview.limit} allowed per operation.`}
            description="Narrow the scope — add a route/bus/driver/trip type filter, or a shorter date range — and try again."
          />
        ) : (
          <Alert
            type="info"
            showIcon
            title={`${mode === 'range' ? 'Matches' : 'Detected period'}: ${preview.from ? formatDate(preview.from) : '?'} to ${preview.to ? formatDate(preview.to) : '?'}`}
            description={
              preview.ineligible > 0
                ? `${preview.eligible} eligible to unassign, ${preview.ineligible} already in progress or completed (will be reported, skipped).`
                : `${preview.eligible} trip(s) will be unassigned.`
            }
          />
        )
      ) : (
        <Typography.Text type="secondary">
          {mode === 'range'
            ? 'Choose a date range to see how many trips match.'
            : mode === 'auto'
              ? 'Select a route, driver or bus above to see the detected period and matching trips.'
              : ' '}
        </Typography.Text>
      )}

      {mode === 'all' && preview && preview.count > 0 && !preview.over_limit && (
        <Checkbox
          checked={acknowledged}
          onChange={(e) => setAcknowledgedForScopeKey(e.target.checked ? scopeKey : null)}
          style={{ marginTop: 12 }}
        >
          I understand this will unassign up to <strong>{preview.eligible}</strong> trip(s) across{' '}
          <strong>all dates</strong>
          {hasEntityFilter ? ' matching the filters above' : ''}.
        </Checkbox>
      )}
    </Modal>
  )
}
