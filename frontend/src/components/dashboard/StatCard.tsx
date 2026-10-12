import type { ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { Progress, Typography } from 'antd'

/** Each metric has its own colour identity (icon tile, progress bar, hover border). */
export type Accent = 'green' | 'blue' | 'purple' | 'orange' | 'red'
/** Colour of the supporting line: only for things that need attention. */
export type NoteTone = 'neutral' | 'success' | 'warning' | 'danger'

/**
 * One operational number: icon, label, value, supporting line, optional progress.
 * The whole card is a button that opens the page explaining the number.
 */
export default function StatCard({
  icon,
  label,
  value,
  suffix,
  note,
  accent,
  noteTone = 'neutral',
  percent,
  to,
}: {
  icon: ReactNode
  label: string
  value: number
  /** e.g. "/ 12" after the value */
  suffix?: string
  note: ReactNode
  accent: Accent
  noteTone?: NoteTone
  /** 0–100, shown as a thin bar; omit when there is nothing to measure. */
  percent?: number
  to: string
}) {
  const navigate = useNavigate()
  return (
    <button
      type="button"
      className={`stat-card stat-card--${accent} stat-card--note-${noteTone}`}
      onClick={() => navigate(to)}
    >
      <span className="stat-card__icon" aria-hidden>
        {icon}
      </span>
      <span className="stat-card__body">
        <span className="stat-card__label">{label}</span>
        <span className="stat-card__value">
          {value.toLocaleString()}
          {suffix && <span className="stat-card__suffix"> {suffix}</span>}
        </span>
        {percent !== undefined && (
          <Progress
            percent={percent}
            size="small"
            showInfo={false}
            strokeColor="var(--accent)"
            railColor="#EEF2F7"
            aria-label={`${label}: ${percent}%`}
          />
        )}
        <Typography.Text className="stat-card__note">{note}</Typography.Text>
      </span>
    </button>
  )
}
