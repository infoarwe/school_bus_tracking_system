import type { ReactNode } from 'react'
import { FlagOutlined, MoonOutlined, SunOutlined } from '@ant-design/icons'
import type { TripType } from '../services/types'

/** Sun for Morning Pickup, moon for Evening Drop, flag for a Special Trip/Outing. */
export const tripTypeIcons: Record<TripType, ReactNode> = {
  morning_pickup: <SunOutlined style={{ color: '#d97706' }} />,
  evening_drop: <MoonOutlined style={{ color: '#4f46e5' }} />,
  special_trip: <FlagOutlined style={{ color: '#7c3aed' }} />,
}
