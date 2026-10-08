import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, App, Button, Card, Flex, Form, Input, Steps, Typography } from 'antd'
import { useAuth } from '../context/AuthContext'
import { authApi } from '../services/auth'
import { errorMessage } from '../utils/formErrors'

type Setup = Awaited<ReturnType<typeof authApi.setup2fa>>

export default function TwoFactorSetupPage() {
  const { me, reloadMe, logout } = useAuth()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const [setup, setSetup] = useState<Setup | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const required = me?.two_factor_setup_required
  const done = me?.user.totp_enabled

  async function start() {
    setBusy(true)
    setError(null)
    try {
      setSetup(await authApi.setup2fa())
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  async function enable(v: { code: string }) {
    setBusy(true)
    setError(null)
    try {
      await authApi.enable2fa(v.code)
      await reloadMe()
      message.success('Two-factor authentication is on.')
      navigate('/', { replace: true })
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Flex align="center" justify="center" className="login-page">
      <Card style={{ width: 460, maxWidth: '100%' }}>
        <Typography.Title level={4} style={{ marginTop: 0 }}>
          Set up two-factor authentication
        </Typography.Title>
        {required && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 16 }}
            title="Super Admin accounts must use an authenticator app before continuing."
          />
        )}
        {done ? (
          <Alert type="success" showIcon title="Two-factor authentication is already on." />
        ) : (
          <>
            <Steps
              orientation="vertical"
              size="small"
              current={setup ? 1 : 0}
              items={[
                {
                  title: 'Scan the QR code',
                  content: 'Use Google Authenticator, Microsoft Authenticator or a similar app.',
                },
                { title: 'Enter the 6-digit code', content: 'This confirms the app is set up.' },
              ]}
            />
            {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
            {!setup ? (
              <Button type="primary" block loading={busy} onClick={() => void start()}>
                Show QR code
              </Button>
            ) : (
              <Flex vertical align="center" gap="middle">
                <img src={setup.qr_code_png} alt="Authenticator QR code" width={200} height={200} />
                <Typography.Text type="secondary" copyable={{ text: setup.secret }}>
                  Can't scan? Key: <code>{setup.secret}</code>
                </Typography.Text>
                <Form layout="inline" onFinish={enable} style={{ justifyContent: 'center' }}>
                  <Form.Item name="code" rules={[{ required: true, len: 6, message: '6 digits' }]}>
                    <Input placeholder="123456" inputMode="numeric" maxLength={6} autoFocus />
                  </Form.Item>
                  <Button type="primary" htmlType="submit" loading={busy}>
                    Turn on
                  </Button>
                </Form>
              </Flex>
            )}
          </>
        )}
        <Flex justify="space-between" style={{ marginTop: 24 }}>
          {!required && <Button onClick={() => navigate('/account')}>Back</Button>}
          <Button type="link" onClick={() => void logout()}>
            Log out
          </Button>
        </Flex>
      </Card>
    </Flex>
  )
}
