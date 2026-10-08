import { createBrowserRouter } from 'react-router-dom'
import AdminLayout from '../layouts/AdminLayout'
import ComingSoon from '../components/ComingSoon'
import DashboardPage from '../pages/DashboardPage'
import NotFoundPage from '../pages/NotFoundPage'
import { menu } from './menu'

// Each menu entry gets a placeholder until its sprint replaces it with a real page.
const placeholders = menu
  .filter((m) => m.path !== '/')
  .map((m) => ({
    path: m.path.slice(1),
    element: <ComingSoon title={m.label} sprint={m.sprint} />,
  }))

export const router = createBrowserRouter([
  {
    path: '/',
    element: <AdminLayout />,
    children: [
      { index: true, element: <DashboardPage /> },
      ...placeholders,
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])
