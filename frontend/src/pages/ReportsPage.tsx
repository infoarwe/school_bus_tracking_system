import { useCallback, useEffect, useState } from 'react'
import { Alert, App, Button, Card, Flex, Input, Select, Table, Typography } from 'antd'
import { DownloadOutlined, SearchOutlined } from '@ant-design/icons'
import LoadingOrError from '../components/LoadingOrError'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import { useCurrentSchool } from '../context/SchoolContext'
import { useLoad } from '../hooks/useLoad'
import { reportsApi, type ReportQuery } from '../services/admin'
import { busesApi, driversApi, routesApi } from '../services/transport'
import { tripsApi } from '../services/trips'
import {
  tripTypeLabels,
  type Bus,
  type BusRoute,
  type Driver,
  type ReportDef,
  type Trip,
} from '../services/types'
import { addDays, todayIn } from '../utils/dates'
import { errorMessage } from '../utils/formErrors'

const PAGE_SIZE = 50

export default function ReportsPage() {
  return <RequireSchool>{(schoolId) => <Reports schoolId={schoolId} />}</RequireSchool>
}

function Reports({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const { school } = useCurrentSchool()
  const today = todayIn(school?.timezone)
  const loadDefs = useCallback(() => reportsApi.list(schoolId), [schoolId])
  const defs = useLoad<ReportDef[]>(loadDefs)

  const [key, setKey] = useState('trips')
  const [query, setQuery] = useState<ReportQuery>({ from: addDays(today, -6), to: today })
  const [tripDay, setTripDay] = useState(today)
  const [masters, setMasters] = useState<{
    routes: BusRoute[]
    buses: Bus[]
    drivers: Driver[]
    trips: Trip[]
  }>({
    routes: [],
    buses: [],
    drivers: [],
    trips: [],
  })
  const [result, setResult] = useState<{ rows: string[][]; total: number; page: number } | null>(
    null,
  )
  const [busy, setBusy] = useState<'run' | 'csv' | null>(null)

  useEffect(() => {
    Promise.all([
      routesApi.list(schoolId, { page_size: 100 }),
      busesApi.list(schoolId, { page_size: 100 }),
      driversApi.list(schoolId, { page_size: 100 }),
    ])
      .then(([r, b, d]) =>
        setMasters((m) => ({ ...m, routes: r.data, buses: b.data, drivers: d.data })),
      )
      .catch(() => {})
  }, [schoolId])

  // Location history works on one trip: list that day's trips to choose from.
  useEffect(() => {
    if (key !== 'location_history') return
    tripsApi
      .list(schoolId, { date: tripDay, page_size: 100 })
      .then((r) => setMasters((m) => ({ ...m, trips: r.data.filter((t) => t.started_at) })))
      .catch(() => {})
  }, [schoolId, key, tripDay])

  const def = defs.data?.find((d) => d.key === key)
  const uses = (f: string) => def?.filters.includes(f as never) ?? false

  function cleanQuery(): ReportQuery {
    const q: ReportQuery = {}
    for (const f of def?.filters ?? []) if (query[f]) q[f] = query[f]
    return q
  }

  async function run(page = 1) {
    if (!def) return
    setBusy('run')
    try {
      const res = await reportsApi.run(schoolId, def.key, cleanQuery(), page, PAGE_SIZE)
      setResult({ rows: res.rows, total: res.meta.total, page })
    } catch (e) {
      setResult(null)
      message.error(errorMessage(e))
    } finally {
      setBusy(null)
    }
  }

  async function csv() {
    if (!def) return
    setBusy('csv')
    try {
      await reportsApi.downloadCsv(schoolId, def.key, cleanQuery())
    } catch (e) {
      message.error(errorMessage(e))
    } finally {
      setBusy(null)
    }
  }

  if (!defs.data) return <LoadingOrError error={defs.error} onRetry={defs.retry} />
  const missingTrip = uses('trip_id') && !query.trip_id

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Reports"
        subtitle="Preview here, or download the full report as CSV (opens in Excel)."
      />
      <Card>
        <Flex vertical gap="middle">
          <Flex gap="small" wrap align="center">
            <Select
              style={{ width: 280 }}
              value={key}
              onChange={(k) => {
                setKey(k)
                setResult(null)
              }}
              options={defs.data.map((d) => ({ value: d.key, label: d.title }))}
            />
            {uses('from') && (
              <>
                <Input
                  type="date"
                  style={{ width: 160 }}
                  value={query.from}
                  max={query.to}
                  onChange={(e) => setQuery({ ...query, from: e.target.value })}
                  aria-label="From"
                />
                <Typography.Text type="secondary">to</Typography.Text>
                <Input
                  type="date"
                  style={{ width: 160 }}
                  value={query.to}
                  min={query.from}
                  onChange={(e) => setQuery({ ...query, to: e.target.value })}
                  aria-label="To"
                />
              </>
            )}
            {uses('route_id') && (
              <Select
                allowClear
                placeholder="All routes"
                style={{ width: 200 }}
                value={query.route_id}
                onChange={(v) => setQuery({ ...query, route_id: v })}
                options={masters.routes.map((r) => ({
                  value: r.id,
                  label: `${r.code} · ${r.name}`,
                }))}
              />
            )}
            {uses('bus_id') && (
              <Select
                allowClear
                placeholder="All buses"
                style={{ width: 180 }}
                value={query.bus_id}
                onChange={(v) => setQuery({ ...query, bus_id: v })}
                options={masters.buses.map((b) => ({ value: b.id, label: b.vehicle_number }))}
              />
            )}
            {uses('driver_id') && (
              <Select
                allowClear
                placeholder="All drivers"
                style={{ width: 180 }}
                value={query.driver_id}
                onChange={(v) => setQuery({ ...query, driver_id: v })}
                options={masters.drivers.map((d) => ({ value: d.id, label: d.name }))}
              />
            )}
            {uses('trip_id') && (
              <>
                <Input
                  type="date"
                  style={{ width: 160 }}
                  value={tripDay}
                  max={today}
                  onChange={(e) => {
                    setTripDay(e.target.value)
                    setQuery({ ...query, trip_id: undefined })
                  }}
                  aria-label="Trip date"
                />
                <Select
                  placeholder="Choose a trip"
                  style={{ width: 300 }}
                  value={query.trip_id}
                  onChange={(v) => setQuery({ ...query, trip_id: v })}
                  notFoundContent="No trips with GPS on this day"
                  options={masters.trips.map((t) => ({
                    value: t.id,
                    label: `${t.route.code} · ${tripTypeLabels[t.trip_type]} · ${t.bus.vehicle_number}`,
                  }))}
                />
              </>
            )}
            <Button
              type="primary"
              icon={<SearchOutlined />}
              loading={busy === 'run'}
              disabled={missingTrip}
              onClick={() => void run(1)}
            >
              Preview
            </Button>
            <Button
              icon={<DownloadOutlined />}
              loading={busy === 'csv'}
              disabled={missingTrip}
              onClick={() => void csv()}
            >
              Download CSV
            </Button>
          </Flex>
          {def && <Typography.Text type="secondary">{def.description}</Typography.Text>}
        </Flex>
      </Card>

      {result && def && (
        <Card title={`${def.title} · ${result.total} row(s)`}>
          {result.total === 0 ? (
            <Alert type="info" showIcon title="No data for these filters." />
          ) : (
            <Table
              size="small"
              rowKey={(_, i) => String(i)}
              dataSource={result.rows}
              scroll={{ x: Math.max(800, def.columns.length * 130) }}
              pagination={{
                current: result.page,
                pageSize: PAGE_SIZE,
                total: result.total,
                showSizeChanger: false,
                onChange: (p) => void run(p),
              }}
              columns={def.columns.map((c, i) => ({
                title: c.label,
                key: c.key,
                render: (_: unknown, row: string[]) => row[i] || '—',
              }))}
            />
          )}
        </Card>
      )}
    </Flex>
  )
}
