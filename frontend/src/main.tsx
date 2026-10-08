import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'
import { App as AntApp, ConfigProvider } from 'antd'
import { AuthProvider } from './context/AuthContext'
import { SchoolProvider } from './context/SchoolContext'
import { router } from './routes/router'
import 'leaflet/dist/leaflet.css'
import './styles/global.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConfigProvider theme={{ token: { colorPrimary: '#f5a623' } }}>
      <AntApp>
        <AuthProvider>
          <SchoolProvider>
            <RouterProvider router={router} />
          </SchoolProvider>
        </AuthProvider>
      </AntApp>
    </ConfigProvider>
  </StrictMode>,
)
