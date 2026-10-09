import { useCallback, useState } from 'react'
import { Alert, App, Button, Card, Descriptions, Flex, Popconfirm, Typography, Upload } from 'antd'
import { UploadOutlined } from '@ant-design/icons'
import LoadingOrError from './LoadingOrError'
import { useLoad } from '../hooks/useLoad'
import { ApiError } from '../services/api'
import { pushSettingsApi, type PushSettings } from '../services/settings'
import { errorMessage } from '../utils/formErrors'

/**
 * The school's own Firebase service-account key, for push notifications from its
 * own branded Parent and Driver apps. Upload the .json downloaded from Firebase.
 */
export default function PushSettingsCard({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const loadPush = useCallback(() => pushSettingsApi.get(schoolId), [schoolId])
  const {
    data: current,
    setData: setCurrent,
    error: loadError,
    retry,
  } = useLoad<PushSettings>(loadPush)
  const [busy, setBusy] = useState<'upload' | 'test' | 'remove' | null>(null)
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string } | null>(null)

  async function upload(file: File) {
    setBusy('upload')
    setTestResult(null)
    try {
      setCurrent(await pushSettingsApi.upload(schoolId, await file.text()))
      message.success('Firebase key saved.')
    } catch (e) {
      // Validation problems explain what is wrong with the file.
      const reason = e instanceof ApiError ? e.fields?.service_account_json : undefined
      message.error(reason ?? errorMessage(e), 8)
    } finally {
      setBusy(null)
    }
  }

  async function test() {
    setBusy('test')
    try {
      setTestResult(await pushSettingsApi.test(schoolId))
    } catch (e) {
      message.error(errorMessage(e))
    } finally {
      setBusy(null)
    }
  }

  async function remove() {
    setBusy('remove')
    setTestResult(null)
    try {
      setCurrent(await pushSettingsApi.remove(schoolId))
      message.success('Firebase key removed.')
    } catch (e) {
      message.error(errorMessage(e))
    } finally {
      setBusy(null)
    }
  }

  return (
    <Card title="Push notifications (Firebase)">
      {!current ? (
        <LoadingOrError error={loadError} onRetry={retry} />
      ) : (
        <Flex vertical gap="middle" style={{ maxWidth: 640 }}>
          <Typography.Paragraph type="secondary" style={{ margin: 0 }}>
            This school's Parent and Driver apps send notifications through the school's own
            Firebase project. In the Firebase console open{' '}
            <b>Project settings → Service accounts → Generate new private key</b> and upload the
            downloaded .json file here. It is stored encrypted and never shown again.
          </Typography.Paragraph>
          {current.configured ? (
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="Firebase project">{current.project_id}</Descriptions.Item>
              <Descriptions.Item label="Service account">{current.client_email}</Descriptions.Item>
              <Descriptions.Item label="Uploaded">
                {current.updated_at && new Date(current.updated_at).toLocaleString()}
              </Descriptions.Item>
            </Descriptions>
          ) : (
            <Alert
              type="warning"
              showIcon
              title="No Firebase key uploaded: this school's push notifications are not delivered to phones."
            />
          )}
          {testResult && (
            <Alert type={testResult.ok ? 'success' : 'error'} showIcon title={testResult.message} />
          )}
          <Flex gap="small" wrap>
            <Upload
              accept=".json,application/json"
              showUploadList={false}
              beforeUpload={(file) => {
                void upload(file)
                return false // we send the file's contents ourselves
              }}
            >
              <Button type="primary" icon={<UploadOutlined />} loading={busy === 'upload'}>
                {current.configured ? 'Replace key' : 'Upload key (.json)'}
              </Button>
            </Upload>
            {current.configured && (
              <>
                <Button loading={busy === 'test'} onClick={() => void test()}>
                  Test with Google
                </Button>
                <Popconfirm
                  title="Remove the Firebase key?"
                  description="Phones will stop receiving this school's push notifications."
                  onConfirm={() => void remove()}
                >
                  <Button danger loading={busy === 'remove'}>
                    Remove
                  </Button>
                </Popconfirm>
              </>
            )}
          </Flex>
        </Flex>
      )}
    </Card>
  )
}
