import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'
import { App as AntApp, ConfigProvider, type ThemeConfig } from 'antd'
import { AuthProvider } from './context/AuthContext'
import { SchoolProvider } from './context/SchoolContext'
import { router } from './routes/router'
import './styles/global.css'
import { tokens } from './styles/tokens'

// Colours come from styles/tokens.ts (also declared as CSS variables in global.css).
// Primary blue #2563EB keeps AA contrast with white button text; bright accents are
// used for fills, darker *Text variants for coloured text.
const theme: ThemeConfig = {
  token: {
    colorPrimary: tokens.primary,
    colorInfo: tokens.primary,
    colorSuccess: tokens.greenText,
    colorWarning: tokens.orangeText,
    colorError: tokens.redText,
    colorBgLayout: tokens.pageBg,
    colorTextBase: tokens.text,
    colorTextSecondary: tokens.textSecondary,
    colorBorder: tokens.border,
    colorBorderSecondary: tokens.border,
    borderRadius: 8,
    fontFamily:
      "Inter, system-ui, -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif",
  },
  components: {
    Layout: {
      siderBg: tokens.sidebar,
      headerBg: tokens.card,
      headerHeight: 64,
      headerPadding: '0 20px',
      bodyBg: tokens.pageBg,
    },
    Table: { headerBg: '#F1F5FB', headerColor: tokens.text, rowHoverBg: '#F6F9FF' },
    Card: { headerFontSize: 16 },
    Button: { fontWeight: 500 },
  },
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConfigProvider theme={theme}>
      <AntApp message={{ maxCount: 3 }}>
        <AuthProvider>
          <SchoolProvider>
            <RouterProvider router={router} />
          </SchoolProvider>
        </AuthProvider>
      </AntApp>
    </ConfigProvider>
  </StrictMode>,
)
