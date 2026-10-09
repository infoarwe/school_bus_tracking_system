import { useCallback, useState } from 'react'
import { Flex, Input, Segmented, Select, Table, Tag, Typography } from 'antd'
import PageHeader from '../components/PageHeader'
import { useAuth } from '../context/AuthContext'
import { useCurrentSchool } from '../context/SchoolContext'
import { useServerList } from '../hooks/useServerList'
import { auditApi, type AuditQuery } from '../services/admin'
import { roleLabels, type AuditRow, type Role } from '../services/types'

const ENTITY_TYPES = [
  'school',
  'user',
  'driver',
  'bus',
  'route',
  'stop',
  'student',
  'parent',
  'trip',
  'emergency',
  'announcement',
  'report',
]

function JsonBlock({ label, value }: { label: string; value: unknown }) {
  if (value === null || value === undefined) return null
  return (
    <div style={{ flex: 1, minWidth: 260 }}>
      <Typography.Text strong>{label}</Typography.Text>
      <pre className="json-block">{JSON.stringify(value, null, 2)}</pre>
    </div>
  )
}

/**
 * Audit log: Super Admin sees every school (or one), School Admin their school,
 * Transport Manager transport entries only (enforced by the API).
 */
export default function AuditLogsPage() {
  const { hasRole } = useAuth()
  const { schoolId, school } = useCurrentSchool()
  const isSuper = hasRole('super_admin')
  const [scope, setScope] = useState<'school' | 'platform'>(
    isSuper && !schoolId ? 'platform' : 'school',
  )
  const usePlatform = isSuper && (scope === 'platform' || !schoolId)

  const fetcher = useCallback(
    (p: AuditQuery) => (usePlatform ? auditApi.platform(p) : auditApi.school(schoolId!, p)),
    [usePlatform, schoolId],
  )
  const list = useServerList(fetcher)

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Audit Logs"
        subtitle={usePlatform ? 'All schools and platform actions' : school?.name}
        extra={
          isSuper &&
          schoolId && (
            <Segmented
              value={scope}
              onChange={(v) => setScope(v as 'school' | 'platform')}
              options={[
                { value: 'school', label: school?.name ?? 'This school' },
                { value: 'platform', label: 'All schools' },
              ]}
            />
          )
        }
      />
      <Flex gap="small" wrap>
        <Input.Search
          allowClear
          placeholder="Action, e.g. trip. or user.suspend"
          style={{ width: 280 }}
          onSearch={(action) => list.update({ action })}
        />
        <Select
          allowClear
          placeholder="Any record type"
          style={{ width: 180 }}
          value={list.params.entity_type}
          onChange={(entity_type) => list.update({ entity_type })}
          options={ENTITY_TYPES.map((t) => ({ value: t, label: t }))}
        />
        <Input
          type="date"
          style={{ width: 160 }}
          aria-label="From"
          onChange={(e) => list.update({ from: e.target.value })}
        />
        <Input
          type="date"
          style={{ width: 160 }}
          aria-label="To"
          onChange={(e) => list.update({ to: e.target.value })}
        />
      </Flex>
      <Table<AuditRow>
        rowKey="id"
        size="small"
        loading={list.loading}
        dataSource={list.rows}
        pagination={list.pagination}
        scroll={{ x: 1000 }}
        expandable={{
          rowExpandable: (r) => r.before !== null || r.after !== null,
          expandedRowRender: (r) => (
            <Flex gap="middle" wrap>
              <JsonBlock label="Before" value={r.before} />
              <JsonBlock label="After" value={r.after} />
            </Flex>
          ),
        }}
        columns={[
          {
            title: 'When',
            dataIndex: 'created_at',
            width: 180,
            render: (v: string) => new Date(v).toLocaleString(),
          },
          {
            title: 'Who',
            render: (_, r) => (
              <>
                {r.actor_name ?? 'System'}
                {r.actor_role && (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {' '}
                    · {roleLabels[r.actor_role as Role] ?? r.actor_role}
                  </Typography.Text>
                )}
              </>
            ),
          },
          { title: 'Action', dataIndex: 'action', render: (a: string) => <Tag>{a}</Tag> },
          { title: 'Record', dataIndex: 'entity_type', width: 120 },
          ...(usePlatform
            ? [
                {
                  title: 'School',
                  dataIndex: 'school_name',
                  render: (v: string | null) => v ?? 'Platform',
                },
              ]
            : []),
          { title: 'IP', dataIndex: 'ip', width: 130 },
        ]}
      />
    </Flex>
  )
}
