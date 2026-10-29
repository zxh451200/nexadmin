/**
 * WGS-84（GPS/OSM）与 GCJ-02（高德/腾讯/国内地图）互转。
 * 偏移算法是公开的通用实现；海外坐标不做偏移（GCJ-02 只在中国境内生效）。
 */

const PI = Math.PI
const A = 6378245.0
const EE = 0.00669342162296594323

function outOfChina(lng: number, lat: number): boolean {
  return lng < 72.004 || lng > 137.8347 || lat < 0.8293 || lat > 55.8271
}

function transformLat(x: number, y: number): number {
  let ret = -100.0 + 2.0 * x + 3.0 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * Math.sqrt(Math.abs(x))
  ret += ((20.0 * Math.sin(6.0 * x * PI) + 20.0 * Math.sin(2.0 * x * PI)) * 2.0) / 3.0
  ret += ((20.0 * Math.sin(y * PI) + 40.0 * Math.sin((y / 3.0) * PI)) * 2.0) / 3.0
  ret += ((160.0 * Math.sin((y / 12.0) * PI) + 320 * Math.sin((y * PI) / 30.0)) * 2.0) / 3.0
  return ret
}

function transformLng(x: number, y: number): number {
  let ret = 300.0 + x + 2.0 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * Math.sqrt(Math.abs(x))
  ret += ((20.0 * Math.sin(6.0 * x * PI) + 20.0 * Math.sin(2.0 * x * PI)) * 2.0) / 3.0
  ret += ((20.0 * Math.sin(x * PI) + 40.0 * Math.sin((x / 3.0) * PI)) * 2.0) / 3.0
  ret += ((150.0 * Math.sin((x / 12.0) * PI) + 300.0 * Math.sin((x / 30.0) * PI)) * 2.0) / 3.0
  return ret
}

function delta(lng: number, lat: number): [number, number] {
  let dLat = transformLat(lng - 105.0, lat - 35.0)
  let dLng = transformLng(lng - 105.0, lat - 35.0)
  const radLat = (lat / 180.0) * PI
  let magic = Math.sin(radLat)
  magic = 1 - EE * magic * magic
  const sqrtMagic = Math.sqrt(magic)
  dLat = (dLat * 180.0) / (((A * (1 - EE)) / (magic * sqrtMagic)) * PI)
  dLng = (dLng * 180.0) / ((A / sqrtMagic) * Math.cos(radLat) * PI)
  return [dLng, dLat]
}

export function wgs84ToGcj02(lng: number, lat: number): [number, number] {
  if (outOfChina(lng, lat)) return [lng, lat]
  const [dLng, dLat] = delta(lng, lat)
  return [lng + dLng, lat + dLat]
}

/** 逆变换用两轮逼近：直接反解没有闭式解，两轮误差已在 1e-6 度（约 0.1 米）内。 */
export function gcj02ToWgs84(lng: number, lat: number): [number, number] {
  if (outOfChina(lng, lat)) return [lng, lat]
  let wLng = lng
  let wLat = lat
  for (let i = 0; i < 2; i++) {
    const [gLng, gLat] = wgs84ToGcj02(wLng, wLat)
    wLng -= gLng - lng
    wLat -= gLat - lat
  }
  return [wLng, wLat]
}

// BD-09（百度）与 GCJ-02 的偏移是两组公开常量，可逆且无迭代。
const BD_FACTOR = (Math.PI * 3000.0) / 180.0

export function gcj02ToBd09(lng: number, lat: number): [number, number] {
  const z = Math.sqrt(lng * lng + lat * lat) + 0.00002 * Math.sin(lat * BD_FACTOR)
  const theta = Math.atan2(lat, lng) + 0.000003 * Math.cos(lng * BD_FACTOR)
  return [z * Math.cos(theta) + 0.0065, z * Math.sin(theta) + 0.006]
}

export function bd09ToGcj02(lng: number, lat: number): [number, number] {
  const x = lng - 0.0065
  const y = lat - 0.006
  const z = Math.sqrt(x * x + y * y) - 0.00002 * Math.sin(y * BD_FACTOR)
  const theta = Math.atan2(y, x) - 0.000003 * Math.cos(x * BD_FACTOR)
  return [z * Math.cos(theta), z * Math.sin(theta)]
}
