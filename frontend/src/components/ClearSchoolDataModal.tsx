import { useEffect, useState } from 'react'
import { Alert, App, Checkbox, Descriptions, Flex, Input, Modal, Spin, Typography } from 'antd'
import { schoolsApi, type ClearDataResult, type ClearScope } from '../services/schools'
import type { School } from '../services/types'
import { errorMessage } from '../utils/formErrors'

const parts: { value: ClearScope; label: string; detail: string; needs?: ClearScope[] }[] = [
  {
    value: 'trips',
    label: 'Daily trips',
    detail: 'All trips with their history, GPS points, stop events, delays and emergencies',
  },
  {
    value: 'notifications',
    label: 'Notifications & announcements',
    detail: 'Sent notifications, inboxes, push queue and announcements',
  },
  {
    value: 'students',
    label: 'Students & parents',
    detail: "Students, parents, parents' app logins and route assignments",
  },
  {
    value: 'routes',
    label: 'Routes & stops',
    detail: 'Routes, stops and driver route assignments',
    needs: ['trips', 'notifications'],
  },
  { value: 'buses', label: 'Buses', detail: 'All buses', needs: ['trips'] },
  {
    value: 'drivers',
    label: 'Drivers',
    detail: 'Drivers and their app logins',
    needs: ['trips'],
  },
]

const countLabels: Record<string, string> = {
  trips: 'Trips',
  gps_points: 'GPS points',
  delay_events: 'Delays',
  emergencies: 'Emergencies',
  notifications: 'Notifications',
  announcements: 'Announcements',
  students: 'Students',
  parents: 'Parents',
  student_assignments: 'Student route assignments',
  routes: 'Routes',
  stops: 'Stops',
  driver_assignments: 'Driver route assignments',
  buses: 'Buses',
  drivers: 'Drivers',
  logins: 'Driver / parent app logins',
}

/** Adds what each chosen part needs (trips reference routes, buses and drivers). */
function withNeeds(chosen: ClearScope[]): ClearScope[] {
  const out = new Set(chosen)
  for (const p of parts) if (out.has(p.value)) p.needs?.forEach((n) => out.add(n))
  return parts.map((p) => p.value).filter((v) => out.has(v))
}

/** Drops parts whose needs were unticked. */
function withoutOrphans(chosen: ClearScope[]): ClearScope[] {
  return chosen.filter((v) =>
    (parts.find((p) => p.value === v)?.needs ?? []).every((n) => chosen.includes(n)),
  )
}

/**
 * Super Admin: clear a school's test data. Shows exactly what would be deleted
 * (dry run), then asks for the school code before deleting (critical action).
 */
export default function ClearSchoolDataModal({
  school,
  onClose,
}: {
  school: School
  onClose: () => void
}) {
  const { message, modal } = App.useApp()
  const [scopes, setScopes] = useState<ClearScope[]>(parts.map((p) => p.value))
  const [preview, setPreview] = useState<ClearDataResult | null>(null)
  const [previewError, setPreviewError] = useState<string | null>(null)
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (scopes.length === 0) return
    let cancelled = false
    schoolsApi
      .clearData(school.id, scopes, { dryRun: true })
      .then((res) => {
        if (cancelled) return
        setPreview(res)
        setPreviewError(null)
      })
      .catch((e: unknown) => !cancelled && setPreviewError(errorMessage(e)))
    return () => {
      cancelled = true
    }
  }, [school.id, scopes])

  const change = (next: ClearScope[]) => {
    const grew = next.length > scopes.length
    setPreview(null)
    setScopes(grew ? withNeeds(next) : withoutOrphans(next))
  }
  const codeOk = code.trim().toUpperCase() === school.code.toUpperCase()
  const counts = Object.entries(preview?.deleted ?? {}).filter(([, n]) => n > 0)

  async function clear() {
    setBusy(true)
    try {
      const res = await schoolsApi.clearData(school.id, scopes, { confirmCode: code.trim() })
      const total = Object.values(res.deleted).reduce((a, b) => a + b, 0)
      message.success(`${school.name}: ${total} record(s) cleared.`)
      onClose()
    } catch (e) {
      modal.error({ title: 'Data was not cleared', content: errorMessage(e) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      title={`Clear data of ${school.name}?`}
      okText="Clear data permanently"
      okButtonProps={{ danger: true, disabled: !codeOk || scopes.length === 0 || !preview }}
      confirmLoading={busy}
      onOk={() => void clear()}
      onCancel={onClose}
      width={640}
      destroyOnHidden
    >
      <Alert
        type="error"
        showIcon
        title="This permanently deletes the chosen data and cannot be undone."
        description="The school, its settings, branding, School Admins, Transport Managers and the audit log are kept. Removed drivers and parents are logged out of the apps."
        style={{ marginBottom: 16 }}
      />
      <Typography.Text strong>What to clear</Typography.Text>
      <Checkbox.Group
        value={scopes}
        onChange={(v) => change(v as ClearScope[])}
        style={{ display: 'flex', flexDirection: 'column', gap: 6, margin: '8px 0 16px' }}
      >
        {parts.map((p) => (
          <Checkbox key={p.value} value={p.value}>
            <Typography.Text strong>{p.label}</Typography.Text>{' '}
            <Typography.Text type="secondary">
              · {p.detail}
              {p.needs && ` (also clears ${p.needs.join(' and ')})`}
            </Typography.Text>
          </Checkbox>
        ))}
      </Checkbox.Group>

      {scopes.length > 0 &&
        (previewError ? (
          <Alert type="warning" showIcon title={previewError} style={{ marginBottom: 16 }} />
        ) : !preview ? (
          <Flex justify="center" style={{ margin: 16 }}>
            <Spin />
          </Flex>
        ) : counts.length === 0 ? (
          <Alert type="info" showIcon title="Nothing to clear." style={{ marginBottom: 16 }} />
        ) : (
          <Descriptions
            size="small"
            bordered
            column={2}
            title="Will be deleted"
            style={{ marginBottom: 16 }}
            items={counts.map(([k, n]) => ({
              key: k,
              label: countLabels[k] ?? k,
              children: n.toLocaleString(),
            }))}
          />
        ))}

      <Typography.Paragraph>
        Type the school code <Typography.Text code>{school.code}</Typography.Text> to confirm.
      </Typography.Paragraph>
      <Input
        value={code}
        onChange={(e) => setCode(e.target.value)}
        placeholder={school.code}
        status={code && !codeOk ? 'error' : undefined}
      />
    </Modal>
  )
}
