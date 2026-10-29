import type { MapProvider } from './types'

/**
 * 地图 provider 注册表：内核只认接口，具体厂商（高德/百度/Mapbox…）由插件注册。
 * 不装任何地图插件时注册表为空，LocationField 退化为手填经纬度 + 提示。
 */
const providers = new Map<string, MapProvider>()

export function registerMapProvider(p: MapProvider) {
  providers.set(p.id, p)
}

export function getMapProviders(): MapProvider[] {
  return [...providers.values()]
}

export function getMapProvider(id: string): MapProvider | undefined {
  return providers.get(id)
}
