import { createBrowserRouter } from 'react-router-dom'
import AdminLayout from '../layouts/AdminLayout'
import ComingSoon from '../components/ComingSoon'
import AccountPage from '../pages/AccountPage'
import AlertsPage from '../pages/AlertsPage'
import AnnouncementsPage from '../pages/AnnouncementsPage'
import AuditLogsPage from '../pages/AuditLogsPage'
import BusesPage from '../pages/BusesPage'
import DashboardPage from '../pages/DashboardPage'
import DriversPage from '../pages/DriversPage'
import ErrorPage from '../pages/ErrorPage'
import LiveTrackingPage from '../pages/LiveTrackingPage'
import LoginPage from '../pages/LoginPage'
import NotFoundPage from '../pages/NotFoundPage'
import ParentsPage from '../pages/ParentsPage'
import ReportsPage from '../pages/ReportsPage'
import RouteDetailPage from '../pages/RouteDetailPage'
import RoutesPage from '../pages/RoutesPage'
import SchoolsPage from '../pages/SchoolsPage'
import SettingsPage from '../pages/SettingsPage'
import StudentImportPage from '../pages/StudentImportPage'
import StudentsPage from '../pages/StudentsPage'
import TripsPage from '../pages/TripsPage'
import TwoFactorSetupPage from '../pages/TwoFactorSetupPage'
import UsersPage from '../pages/UsersPage'
import { RequireAuth, RoleGate } from './guards'
import { menu } from './menu'

// Pages that exist; every other menu entry shows a placeholder until its sprint.
const pages: Record<string, React.ReactNode> = {
  '/': <DashboardPage />,
  '/schools': <SchoolsPage />,
  '/users': <UsersPage />,
  '/drivers': <DriversPage />,
  '/buses': <BusesPage />,
  '/routes': <RoutesPage />,
  '/students': <StudentsPage />,
  '/parents': <ParentsPage />,
  '/settings': <SettingsPage />,
  '/trips': <TripsPage />,
  '/live': <LiveTrackingPage />,
  '/alerts': <AlertsPage />,
  '/announcements': <AnnouncementsPage />,
  '/reports': <ReportsPage />,
  '/audit-logs': <AuditLogsPage />,
}

const menuRoutes = menu.map((m) => {
  const element = (
    <RoleGate roles={m.roles}>
      {pages[m.path] ?? <ComingSoon title={m.label} sprint={m.sprint} />}
    </RoleGate>
  )
  return m.path === '/' ? { index: true, element } : { path: m.path.slice(1), element }
})

export const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  {
    path: '/setup-2fa',
    element: (
      <RequireAuth>
        <TwoFactorSetupPage />
      </RequireAuth>
    ),
  },
  {
    path: '/',
    element: (
      <RequireAuth>
        <AdminLayout />
      </RequireAuth>
    ),
    errorElement: <ErrorPage />,
    children: [
      ...menuRoutes,
      {
        path: 'routes/:routeId',
        element: (
          <RoleGate roles={['super_admin', 'school_admin', 'transport_manager']}>
            <RouteDetailPage />
          </RoleGate>
        ),
      },
      {
        path: 'students/import',
        element: (
          <RoleGate roles={['super_admin', 'school_admin']}>
            <StudentImportPage />
          </RoleGate>
        ),
      },
      { path: 'account', element: <AccountPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])
