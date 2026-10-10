import { useCallback, useState } from 'react'
import {
  App,
  Button,
  Card,
  ColorPicker,
  Flex,
  Form,
  Input,
  Popconfirm,
  Space,
  Typography,
  Upload,
  type FormInstance,
} from 'antd'
import { UploadOutlined } from '@ant-design/icons'
import LoadingOrError from './LoadingOrError'
import { useLoad } from '../hooks/useLoad'
import { ApiError, apiUrl } from '../services/api'
import { brandingApi, type Branding } from '../services/settings'
import { applyApiErrors, errorMessage } from '../utils/formErrors'

const HEX = /^#[0-9A-Fa-f]{6}$/

interface Values {
  app_name: string
  primary_color: string
  secondary_color: string
}

/**
 * The school's white-label Parent and Driver apps read their name, colours and
 * logo from the API (GET /branding), so changes here reach the apps without a new build.
 */
export default function BrandingSettingsCard({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const [form] = Form.useForm<Values>()
  const loadBranding = useCallback(() => brandingApi.get(schoolId), [schoolId])
  const { data: current, setData: setCurrent, error: loadError, retry } = useLoad(loadBranding)
  const [busy, setBusy] = useState<'save' | 'upload' | 'remove' | null>(null)
  const appName = Form.useWatch('app_name', form)
  const primary = Form.useWatch('primary_color', form)

  async function save(v: Values) {
    setBusy('save')
    try {
      const saved = await brandingApi.update(schoolId, {
        app_name: v.app_name ?? '',
        primary_color: v.primary_color ?? '',
        secondary_color: v.secondary_color ?? '',
      })
      setCurrent(saved)
      form.setFieldsValue(formValues(saved))
      message.success('Branding saved. The apps pick it up the next time they open.')
    } catch (e) {
      const msg = applyApiErrors(form, e)
      if (msg) message.error(msg)
    } finally {
      setBusy(null)
    }
  }

  async function upload(file: File) {
    setBusy('upload')
    try {
      setCurrent(await brandingApi.uploadLogo(schoolId, file))
      message.success('Logo saved.')
    } catch (e) {
      const reason = e instanceof ApiError ? e.fields?.logo : undefined
      message.error(reason ? `Logo ${reason}.` : errorMessage(e), 8)
    } finally {
      setBusy(null)
    }
  }

  async function removeLogo() {
    setBusy('remove')
    try {
      setCurrent(await brandingApi.removeLogo(schoolId))
      message.success('Logo removed.')
    } catch (e) {
      message.error(errorMessage(e))
    } finally {
      setBusy(null)
    }
  }

  return (
    <Card title="App branding">
      {!current ? (
        <LoadingOrError error={loadError} onRetry={retry} />
      ) : (
        <Flex vertical gap="middle" style={{ maxWidth: 640 }}>
          <Typography.Paragraph type="secondary" style={{ margin: 0 }}>
            The name, colours and logo shown in this school's Parent and Driver apps. Leave a field
            empty to use the app's default.
          </Typography.Paragraph>

          <Flex align="center" gap="middle" wrap>
            <div
              aria-label="Preview"
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 12,
                padding: '10px 16px',
                borderRadius: 8,
                minWidth: 260,
                background: HEX.test(primary ?? '') ? primary : '#1677FF',
                color: '#fff',
              }}
            >
              {current.logo_url && (
                <img
                  src={apiUrl(current.logo_url)}
                  alt="School logo"
                  style={{
                    height: 40,
                    width: 40,
                    objectFit: 'contain',
                    background: '#fff',
                    borderRadius: 6,
                  }}
                />
              )}
              <b>{appName || current.school_name}</b>
            </div>
            <Space wrap>
              <Upload
                accept=".png,.jpg,.jpeg,.webp,image/png,image/jpeg,image/webp"
                showUploadList={false}
                beforeUpload={(file) => {
                  void upload(file)
                  return false // we send the file ourselves
                }}
              >
                <Button icon={<UploadOutlined />} loading={busy === 'upload'}>
                  {current.logo_url ? 'Replace logo' : 'Upload logo'}
                </Button>
              </Upload>
              {current.logo_url && (
                <Popconfirm title="Remove the logo?" onConfirm={() => void removeLogo()}>
                  <Button danger loading={busy === 'remove'}>
                    Remove logo
                  </Button>
                </Popconfirm>
              )}
            </Space>
          </Flex>
          <Typography.Text type="secondary">
            Logo: PNG, JPEG or WebP, square works best (e.g. 512×512), at most 1 MB.
          </Typography.Text>

          <Form form={form} layout="vertical" initialValues={formValues(current)} onFinish={save}>
            <Form.Item
              name="app_name"
              label="App name"
              extra={`Empty: the apps show "${current.school_name}".`}
            >
              <Input maxLength={60} showCount allowClear placeholder={current.school_name} />
            </Form.Item>
            <ColorField form={form} name="primary_color" label="Main colour" />
            <ColorField form={form} name="secondary_color" label="Accent colour" />
            <Button type="primary" htmlType="submit" loading={busy === 'save'}>
              Save
            </Button>
          </Form>
        </Flex>
      )}
    </Card>
  )
}

function formValues(b: Branding): Values {
  return {
    app_name: b.app_name,
    primary_color: b.primary_color ?? '',
    secondary_color: b.secondary_color ?? '',
  }
}

function ColorField({
  form,
  name,
  label,
}: {
  form: FormInstance<Values>
  name: 'primary_color' | 'secondary_color'
  label: string
}) {
  const value = Form.useWatch(name, form)
  return (
    <Form.Item label={label} required={false}>
      <Space.Compact>
        <ColorPicker
          disabledAlpha
          format="hex"
          value={HEX.test(value ?? '') ? value : undefined}
          onChange={(c) => form.setFieldValue(name, c.toHexString().toUpperCase())}
        />
        <Form.Item
          name={name}
          noStyle
          rules={[{ pattern: /^(#[0-9A-Fa-f]{6})?$/, message: 'Use a colour like #1A73E8' }]}
        >
          <Input placeholder="#1A73E8 (empty = default)" allowClear style={{ width: 240 }} />
        </Form.Item>
      </Space.Compact>
    </Form.Item>
  )
}
