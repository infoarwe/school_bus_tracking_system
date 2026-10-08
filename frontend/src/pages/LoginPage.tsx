import { useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Alert, Button, Card, Flex, Form, Input, Typography } from 'antd'
import { LockOutlined, MailOutlined, SafetyOutlined } from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import { errorMessage } from '../utils/formErrors'

export default function LoginPage() {
  const { state, login, complete2fa } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [mfaToken, setMfaToken] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const from = (location.state as { from?: string } | null)?.from ?? '/'
  if (state.status === 'authenticated') return <Navigate to={from} replace />

  async function run(fn: () => Promise<void>) {
    setBusy(true)
    setError(null)
    try {
      await fn()
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  const onPassword = (v: { email: string; password: string }) =>
    run(async () => {
      const res = await login(v.email, v.password)
      if (res.mfaToken) setMfaToken(res.mfaToken)
      else navigate(from, { replace: true })
    })

  const onCode = (v: { code: string }) =>
    run(async () => {
      await complete2fa(mfaToken!, v.code)
      navigate(from, { replace: true })
    })

  return (
    <Flex align="center" justify="center" className="login-page">
      <Card style={{ width: 380, maxWidth: '100%' }}>
        <Typography.Title level={3} style={{ marginTop: 0 }}>
          School Bus Tracking
        </Typography.Title>
        <Typography.Paragraph type="secondary">
          {mfaToken ? 'Enter the 6-digit code from your authenticator app.' : 'Admin login'}
        </Typography.Paragraph>
        {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}

        {mfaToken ? (
          <Form layout="vertical" onFinish={onCode} requiredMark={false}>
            <Form.Item
              name="code"
              rules={[{ required: true, len: 6, message: 'Enter the 6-digit code' }]}
            >
              <Input
                prefix={<SafetyOutlined />}
                placeholder="123456"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                autoFocus
              />
            </Form.Item>
            <Button type="primary" htmlType="submit" block loading={busy}>
              Verify
            </Button>
            <Button type="link" block onClick={() => setMfaToken(null)}>
              Back
            </Button>
          </Form>
        ) : (
          <Form layout="vertical" onFinish={onPassword} requiredMark={false}>
            <Form.Item name="email" label="Email" rules={[{ required: true, type: 'email' }]}>
              <Input prefix={<MailOutlined />} autoComplete="username" autoFocus />
            </Form.Item>
            <Form.Item name="password" label="Password" rules={[{ required: true }]}>
              <Input.Password prefix={<LockOutlined />} autoComplete="current-password" />
            </Form.Item>
            <Button type="primary" htmlType="submit" block loading={busy}>
              Log in
            </Button>
          </Form>
        )}
      </Card>
    </Flex>
  )
}
