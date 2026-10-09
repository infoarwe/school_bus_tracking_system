import { useCallback, useEffect, useState } from 'react'
import {
  App,
  Button,
  Flex,
  Form,
  Input,
  Modal,
  Popconfirm,
  Radio,
  Select,
  Table,
  Tag,
  Typography,
} from 'antd'
import { NotificationOutlined } from '@ant-design/icons'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import { useAuth } from '../context/AuthContext'
import { useServerList } from '../hooks/useServerList'
import { announcementsApi } from '../services/alerts'
import { routesApi } from '../services/transport'
import {
  announcementCategoryLabels,
  type Announcement,
  type AnnouncementCategory,
  type BusRoute,
  type ListParams,
} from '../services/types'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

const statusColors = {
  scheduled: 'blue',
  sending: 'processing',
  sent: 'green',
  cancelled: 'default',
}
const when = (iso: string | null) => (iso ? new Date(iso).toLocaleString() : '')

export default function AnnouncementsPage() {
  return <RequireSchool>{(schoolId) => <Announcements schoolId={schoolId} />}</RequireSchool>
}

function Announcements({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const fetcher = useCallback((p: ListParams) => announcementsApi.list(schoolId, p), [schoolId])
  const list = useServerList(fetcher)
  const [composing, setComposing] = useState(false)

  async function cancel(a: Announcement) {
    try {
      await announcementsApi.cancel(schoolId, a.id)
      message.success('Scheduled announcement cancelled.')
      list.reload()
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  return (
    <Flex vertical gap="middle">
      <PageHeader
        title="Announcements"
        subtitle="Push a message to every parent of the school, or to the parents of one route."
        extra={
          <Button type="primary" icon={<NotificationOutlined />} onClick={() => setComposing(true)}>
            New announcement
          </Button>
        }
      />
      <Select
        style={{ width: 180 }}
        value={list.params.status ?? ''}
        onChange={(status) => list.update({ status })}
        options={[
          { value: '', label: 'All' },
          { value: 'scheduled', label: 'Scheduled' },
          { value: 'sent', label: 'Sent' },
          { value: 'cancelled', label: 'Cancelled' },
        ]}
      />
      <Table<Announcement>
        rowKey="id"
        loading={list.loading}
        dataSource={list.rows}
        pagination={list.pagination}
        scroll={{ x: 1000 }}
        expandable={{
          expandedRowRender: (a) => (
            <>
              <Typography.Paragraph style={{ whiteSpace: 'pre-wrap' }}>
                {a.message}
              </Typography.Paragraph>
              {a.attachment_url && (
                <a href={a.attachment_url} target="_blank" rel="noreferrer">
                  Attachment
                </a>
              )}
            </>
          ),
        }}
        columns={[
          { title: 'Title', dataIndex: 'title' },
          {
            title: 'To',
            width: 140,
            render: (_, a) =>
              a.target === 'school' ? (
                <Tag color="purple">Whole school</Tag>
              ) : (
                <Tag color="gold">{a.route_code}</Tag>
              ),
          },
          {
            title: 'Category',
            dataIndex: 'category',
            render: (c: AnnouncementCategory) => announcementCategoryLabels[c],
          },
          {
            title: 'Status',
            width: 220,
            render: (_, a) => (
              <>
                <Tag color={statusColors[a.status]}>{a.status}</Tag>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {a.status === 'sent'
                    ? when(a.sent_at)
                    : a.status === 'scheduled'
                      ? when(a.scheduled_at)
                      : ''}
                </Typography.Text>
              </>
            ),
          },
          {
            title: 'Delivery',
            width: 230,
            render: (_, a) =>
              a.stats ? (
                <Typography.Text style={{ fontSize: 12 }}>
                  {a.stats.recipients} parents · {a.stats.sent} pushed
                  {a.stats.failed > 0 && (
                    <Typography.Text type="danger"> · {a.stats.failed} failed</Typography.Text>
                  )}
                  {a.stats.pending > 0 && ` · ${a.stats.pending} sending`} · {a.stats.read} read
                </Typography.Text>
              ) : a.status === 'sent' ? (
                <Typography.Text type="secondary">No parents to notify</Typography.Text>
              ) : null,
          },
          { title: 'By', dataIndex: 'created_by_name', width: 160 },
          {
            title: '',
            width: 100,
            render: (_, a) =>
              a.status === 'scheduled' && (
                <Popconfirm
                  title="Cancel this scheduled announcement?"
                  onConfirm={() => void cancel(a)}
                >
                  <Button size="small" danger>
                    Cancel
                  </Button>
                </Popconfirm>
              ),
          },
        ]}
      />
      {composing && (
        <Composer
          schoolId={schoolId}
          onClose={() => setComposing(false)}
          onSent={() => {
            setComposing(false)
            list.reload()
          }}
        />
      )}
    </Flex>
  )
}

interface ComposerValues {
  target: 'school' | 'route'
  route_id?: string
  category: AnnouncementCategory
  title: string
  message: string
  attachment_url?: string
  when: 'now' | 'later'
  scheduled_at?: string // datetime-local value
}

function Composer({
  schoolId,
  onClose,
  onSent,
}: {
  schoolId: string
  onClose: () => void
  onSent: () => void
}) {
  const { message } = App.useApp()
  const { hasRole } = useAuth()
  // Whole-school announcements are for Super Admin and School Admin (permission matrix).
  const canSchool = hasRole('super_admin', 'school_admin')
  const [form] = Form.useForm<ComposerValues>()
  const [routes, setRoutes] = useState<BusRoute[]>([])
  const [saving, setSaving] = useState(false)
  const target = Form.useWatch('target', form)
  const whenValue = Form.useWatch('when', form)

  useEffect(() => {
    routesApi
      .list(schoolId, { status: 'active', page_size: 100 })
      .then((r) => setRoutes(r.data))
      .catch(() => setRoutes([]))
  }, [schoolId])

  async function save(v: ComposerValues) {
    setSaving(true)
    try {
      const res = await announcementsApi.create(schoolId, {
        category: v.category,
        target: v.target,
        route_id: v.target === 'route' ? v.route_id : null,
        title: v.title,
        message: v.message,
        attachment_url: v.attachment_url || '',
        scheduled_at:
          v.when === 'later' && v.scheduled_at ? new Date(v.scheduled_at).toISOString() : null,
      })
      message.success(
        res.status === 'scheduled'
          ? 'Announcement scheduled.'
          : `Sent to ${res.stats?.recipients ?? 0} parent(s).`,
      )
      onSent()
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
      title="New announcement"
      okText={whenValue === 'later' ? 'Schedule' : 'Send now'}
      confirmLoading={saving}
      onOk={() => form.submit()}
      onCancel={onClose}
      width={640}
      destroyOnHidden
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={save}
        initialValues={{
          target: canSchool ? 'school' : 'route',
          category: canSchool ? 'school_announcement' : 'route_announcement',
          when: 'now',
        }}
      >
        <Form.Item name="target" label="Send to">
          <Radio.Group
            options={[
              { value: 'school', label: 'Whole school', disabled: !canSchool },
              { value: 'route', label: 'One route' },
            ]}
          />
        </Form.Item>
        {target === 'route' && (
          <Form.Item name="route_id" label="Route" rules={[{ required: true }]}>
            <Select
              showSearch={{ optionFilterProp: 'label' }}
              options={routes.map((r) => ({ value: r.id, label: `${r.code} · ${r.name}` }))}
            />
          </Form.Item>
        )}
        <Form.Item name="category" label="Category" rules={[{ required: true }]}>
          <Select
            options={Object.entries(announcementCategoryLabels).map(([value, label]) => ({
              value,
              label,
            }))}
          />
        </Form.Item>
        <Form.Item name="title" label="Title" rules={[{ required: true, max: 100 }]}>
          <Input placeholder="School closed tomorrow" />
        </Form.Item>
        <Form.Item name="message" label="Message" rules={[{ required: true, max: 1000 }]}>
          <Input.TextArea rows={4} showCount maxLength={1000} />
        </Form.Item>
        <Form.Item
          name="attachment_url"
          label="Attachment link (optional)"
          rules={[{ type: 'url', message: 'Enter a web link (https://…)' }]}
        >
          <Input placeholder="https://…" />
        </Form.Item>
        <Form.Item name="when" label="When">
          <Radio.Group
            options={[
              { value: 'now', label: 'Send now' },
              { value: 'later', label: 'Schedule' },
            ]}
          />
        </Form.Item>
        {whenValue === 'later' && (
          <Form.Item name="scheduled_at" label="Send at" rules={[{ required: true }]}>
            <Input type="datetime-local" />
          </Form.Item>
        )}
      </Form>
    </Modal>
  )
}
