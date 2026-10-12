import { Link, NavLink } from 'react-router-dom'
import { Tooltip } from 'antd'
import { CloseOutlined, MenuFoldOutlined, MenuUnfoldOutlined } from '@ant-design/icons'
import type { MenuEntry, MenuGroup } from '../routes/menu'

const GROUPS: MenuGroup[] = [
  'Overview',
  'Operations',
  'Transport setup',
  'People',
  'Administration',
]

/** The product mark: a bus drawn as an outline icon (no emoji, renders the same everywhere). */
function BusMark() {
  return (
    <svg
      viewBox="0 0 24 24"
      width="20"
      height="20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      focusable="false"
    >
      <rect x="4" y="3" width="16" height="15" rx="3" />
      <path d="M4 11h16M8 3v8M16 3v8" />
      <circle cx="8" cy="15" r="1" />
      <circle cx="16" cy="15" r="1" />
      <path d="M7 18v2M17 18v2" />
    </svg>
  )
}

/**
 * Sidebar navigation for the admin web: grouped links with an active state from the
 * URL (NavLink), tooltips when collapsed, and a collapse button (desktop) or close
 * button (mobile drawer). Only entries the caller's role may see are passed in.
 */
export default function SidebarNav({
  entries,
  collapsed,
  onToggle,
  onClose,
  onNavigate,
}: {
  entries: MenuEntry[]
  collapsed: boolean
  /** Desktop: collapse / expand. */
  onToggle?: () => void
  /** Mobile drawer: close. */
  onClose?: () => void
  onNavigate?: () => void
}) {
  return (
    <nav className={`sidenav${collapsed ? ' sidenav--collapsed' : ''}`} aria-label="Main">
      <div className="sidenav__top">
        <Link
          to="/"
          className="sidenav__brand"
          onClick={onNavigate}
          aria-label="School Bus Tracking: dashboard"
        >
          <span className="sidenav__logo">
            <BusMark />
          </span>
          {!collapsed && (
            <span className="sidenav__name">
              School Bus
              <small>Tracking</small>
            </span>
          )}
        </Link>
        {onClose && (
          <button
            type="button"
            className="sidenav__icon-button"
            onClick={onClose}
            aria-label="Close menu"
          >
            <CloseOutlined />
          </button>
        )}
      </div>

      <div className="sidenav__scroll">
        {GROUPS.map((group) => {
          const items = entries.filter((m) => m.group === group)
          if (items.length === 0) return null
          return (
            <div key={group} className="sidenav__group" role="group" aria-label={group}>
              {collapsed ? (
                <div className="sidenav__divider" aria-hidden />
              ) : (
                <div className="sidenav__heading">{group}</div>
              )}
              <ul className="sidenav__list">
                {items.map((m) => (
                  <li key={m.path}>
                    <Tooltip
                      title={collapsed ? m.label : undefined}
                      placement="right"
                      mouseEnterDelay={0.05}
                    >
                      <NavLink
                        to={m.path}
                        end={m.path === '/'}
                        onClick={onNavigate}
                        aria-label={collapsed ? m.label : undefined}
                        className={({ isActive }) =>
                          `sidenav__link${isActive ? ' sidenav__link--active' : ''}`
                        }
                      >
                        <span className="sidenav__link-icon" aria-hidden>
                          {m.icon}
                        </span>
                        {!collapsed && <span className="sidenav__link-label">{m.label}</span>}
                      </NavLink>
                    </Tooltip>
                  </li>
                ))}
              </ul>
            </div>
          )
        })}
      </div>

      {onToggle && (
        <button
          type="button"
          className="sidenav__collapse"
          onClick={onToggle}
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          aria-expanded={!collapsed}
        >
          <span className="sidenav__link-icon" aria-hidden>
            {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
          </span>
          {!collapsed && <span className="sidenav__link-label">Collapse</span>}
        </button>
      )}
    </nav>
  )
}
