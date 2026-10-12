import { useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Alert, Button, Checkbox, ConfigProvider, Form, Input, type ThemeConfig } from 'antd'
import {
  BellOutlined,
  CarOutlined,
  CheckCircleOutlined,
  EnvironmentOutlined,
  LockOutlined,
  MailOutlined,
  SafetyCertificateOutlined,
  SafetyOutlined,
  SendOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import { errorMessage } from '../utils/formErrors'
import '../styles/login.css'

// "Remember me" keeps the email on this device. Sessions themselves are unchanged.
const EMAIL_KEY = 'sbt.loginEmail'

function readEmail(): string {
  try {
    return localStorage.getItem(EMAIL_KEY) ?? ''
  } catch {
    return ''
  }
}

function rememberEmail(email: string | null) {
  try {
    if (email) localStorage.setItem(EMAIL_KEY, email)
    else localStorage.removeItem(EMAIL_KEY)
  } catch {
    // storage unavailable: nothing to remember
  }
}

// Login colours (navy text, bright blue actions), for this page only.
const loginTheme: ThemeConfig = {
  token: { colorPrimary: '#0866F5', colorText: '#10265A', borderRadius: 10 },
  components: { Input: { activeShadow: '0 0 0 3px rgba(8, 102, 245, 0.15)' } },
}

const FEATURES = [
  {
    tone: 'green',
    icon: <EnvironmentOutlined />,
    title: 'Live Bus Tracking',
    text: 'Real-time GPS location',
  },
  {
    tone: 'purple',
    icon: <SafetyCertificateOutlined />,
    title: 'Student Safety',
    text: 'Safer journeys for every child',
  },
  {
    tone: 'orange',
    icon: <BellOutlined />,
    title: 'Instant Notifications',
    text: 'Keep parents informed',
  },
]

const STATS = [
  { tone: 'blue', icon: <TeamOutlined />, value: '1000+', label: 'Students Travel Safely' },
  { tone: 'orange', icon: <CarOutlined />, value: '50+', label: 'Active Buses' },
  { tone: 'green', icon: <CheckCircleOutlined />, value: '24/7', label: 'Monitoring & Support' },
]

export default function LoginPage() {
  const { state, login, complete2fa } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [mfaToken, setMfaToken] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [savedEmail] = useState(readEmail)

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

  const onPassword = (v: { email: string; password: string; remember: boolean }) =>
    run(async () => {
      const res = await login(v.email, v.password)
      rememberEmail(v.remember ? v.email : null)
      if (res.mfaToken) setMfaToken(res.mfaToken)
      else navigate(from, { replace: true })
    })

  const onCode = (v: { code: string }) =>
    run(async () => {
      await complete2fa(mfaToken!, v.code)
      navigate(from, { replace: true })
    })

  return (
    <ConfigProvider theme={loginTheme}>
      <div className="auth">
        <section className="auth-visual" aria-label="About School Bus Tracking">
          {/* Photo: public/images/front_school.png, unchanged — set in login.css */}
          <div className="auth-visual__photo" aria-hidden />
          <div className="auth-visual__overlay" aria-hidden />
          <div className="auth-visual__content">
            <div className="auth-visual__brand">School Bus Tracking</div>

            <div className="auth-visual__pitch">
              <h2 className="auth-visual__heading">
                Safe Journeys.
                <br />
                Brighter <span>Tomorrows.</span>
              </h2>
              <p className="auth-visual__lead">
                Real-time school bus tracking for safer students, happier parents and smarter
                schools.
              </p>
              <ul className="auth-features">
                {FEATURES.map((f) => (
                  <li key={f.title} className="auth-feature">
                    <span className={`auth-icon auth-icon--${f.tone}`} aria-hidden>
                      {f.icon}
                    </span>
                    <span>
                      <strong>{f.title}</strong>
                      <small>{f.text}</small>
                    </span>
                  </li>
                ))}
              </ul>
            </div>

            <ul className="auth-stats">
              {STATS.map((s) => (
                <li key={s.label} className="auth-stat">
                  <span className={`auth-icon auth-icon--${s.tone}`} aria-hidden>
                    {s.icon}
                  </span>
                  <span>
                    <strong>{s.value}</strong>
                    <small>{s.label}</small>
                  </span>
                </li>
              ))}
            </ul>
          </div>
        </section>

        <main className="auth-main">
          <div className="auth-card">
            <div className="auth-card__head">
              <h1 className="auth-card__product">School Bus Tracking</h1>
              <span className="auth-card__accent" aria-hidden />
              <h2 className="auth-card__title">
                {mfaToken ? 'Two-step verification' : 'Welcome Back'}
              </h2>
              <p className="auth-card__text">
                {mfaToken
                  ? 'Enter the 6-digit code from your authenticator app.'
                  : 'Sign in to manage your school transportation.'}
              </p>
            </div>

            {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}

            {mfaToken ? (
              <Form layout="vertical" onFinish={onCode} requiredMark={false} size="large">
                <Form.Item
                  name="code"
                  label="Verification code"
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
                <Button
                  type="primary"
                  htmlType="submit"
                  block
                  loading={busy}
                  className="auth-submit"
                >
                  Verify
                </Button>
                <Button
                  type="link"
                  block
                  onClick={() => setMfaToken(null)}
                  style={{ marginTop: 8 }}
                >
                  Back
                </Button>
              </Form>
            ) : (
              <Form
                layout="vertical"
                onFinish={onPassword}
                requiredMark={false}
                size="large"
                initialValues={{ email: savedEmail, remember: !!savedEmail }}
              >
                <Form.Item
                  name="email"
                  label="Email"
                  rules={[
                    { required: true, message: 'Enter your email address' },
                    { type: 'email', message: 'Enter a valid email address' },
                  ]}
                >
                  <Input
                    prefix={<MailOutlined />}
                    placeholder="Enter your email address"
                    autoComplete="username"
                    autoFocus={!savedEmail}
                  />
                </Form.Item>
                <Form.Item
                  name="password"
                  label="Password"
                  rules={[{ required: true, message: 'Enter your password' }]}
                >
                  <Input.Password
                    prefix={<LockOutlined />}
                    placeholder="Enter your password"
                    autoComplete="current-password"
                    autoFocus={!!savedEmail}
                  />
                </Form.Item>
                <Form.Item name="remember" valuePropName="checked" className="auth-remember">
                  <Checkbox>Remember me</Checkbox>
                </Form.Item>
                <Button
                  type="primary"
                  htmlType="submit"
                  block
                  loading={busy}
                  icon={<SendOutlined />}
                  className="auth-submit"
                >
                  Sign In
                </Button>
              </Form>
            )}
          </div>
        </main>
      </div>
    </ConfigProvider>
  )
}
