import type { ComponentType } from 'react'

/** 一个地理点位。system 标明坐标系：高德/腾讯 gcj02，百度 bd09，OSM/GPS wgs84。 */
export interface GeoPoint {
  lng: number
  lat: number
  address?: string
  system?: 'wgs84' | 'gcj02' | 'bd09'
}

export interface MapPickerProps {
  value?: GeoPoint | null
  onChange: (p: GeoPoint) => void
  /** 地图可视高度（px） */
  height?: number
}

/** 地图服务提供方：由插件在启动时注册（见 framework/map/loader.ts）。 */
export interface MapProvider {
  id: string
  title: string
  Picker: ComponentType<MapPickerProps>
}
