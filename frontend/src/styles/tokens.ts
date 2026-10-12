/**
 * Design tokens: the one place colours are defined. `main.tsx` feeds them to the antd
 * theme and `global.css` declares the same values as CSS variables (keep both in sync).
 *
 * Accents (icons, fills, pills) use the bright values; text uses the `*Text` variants,
 * which keep WCAG AA contrast on white (e.g. #F59E0B orange is only 2.1:1 as text).
 */
export const tokens = {
  sidebar: '#101B30',
  sidebarHover: '#1A2740',
  sidebarText: '#A7B3C8',
  primary: '#2563EB',
  primaryText: '#1D4ED8',
  pageBg: '#F3F6FC',
  card: '#FFFFFF',
  green: '#16A765',
  greenText: '#0F7A49',
  purple: '#7C3AED',
  orange: '#F59E0B',
  orangeText: '#B45309',
  red: '#EF4444',
  redText: '#DC2626',
  text: '#17243A',
  // #718096 is 4.0:1 on white (below AA for small text); this is the nearest passing shade.
  textSecondary: '#5F6D84',
  border: '#E5EAF3',
} as const
