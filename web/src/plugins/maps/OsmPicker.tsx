import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import type { MapPickerProps } from '@/framework/map'
import { regeo, useMapsConfig } from './api'
import { PickerSearch } from './PickerSearch'

// Leaflet 全局对象；只声明用到的部分。SDK 与样式从 CDN 加载，瓦片用 OSM 官方服务器——
// 免 Key 免费方案：国内网络访问官方瓦片可能偏慢，自建瓦片/Nominatim 时改这里的地址即可。
type LeafletMap = {
  on: (ev: string, fn: (e: { latlng: { lat: number; lng: number } }) => void) => void
  setView: (center: [number, number], zoom?: number) => void
  remove: () => void
}
type LeafletMarker = {
  setLatLng: (p: [number, number]) => void
  getLatLng: () => { lat: number; lng: number }
  addTo: (m: LeafletMap) => LeafletMarker
  on: (ev: string, fn: () => void) => void
}
type LeafletNS = {
  map: (el: HTMLElement, opts: Record<string, unknown>) => LeafletMap
  tileLayer: (url: string, opts: Record<string, unknown>) => { addTo: (m: LeafletMap) => void }
  marker: (latlng: [number, number], opts?: Record<string, unknown>) => LeafletMarker
  divIcon: (opts: Record<string, unknown>) => unknown
}

declare global {
  interface Window {
    L?: LeafletNS
  }
}

const LEAFLET_VERSION = '1.9.4'

let leafletSdk: Promise<LeafletNS> | null = null

/** 加载一次 Leaflet（JS + CSS）；失败时错误留在 promise 上，调用方兜成提示。 */
function loadLeaflet(): Promise<LeafletNS> {
  if (window.L) return Promise.resolve(window.L)
  if (leafletSdk) return leafletSdk
  leafletSdk = new Promise<LeafletNS>((resolve, reject) => {
    const base = `https://unpkg.com/leaflet@${LEAFLET_VERSION}/dist`
    const css = document.createElement('link')
    css.rel = 'stylesheet'
    css.href = `${base}/leaflet.min.css`
    css.onerror = () => reject(new Error('Leaflet: css load failed'))
    document.head.appendChild(css)
    const script = document.createElement('script')
    script.async = true
    script.src = `${base}/leaflet.js`
    // Leaflet 挂的全局是 window.L（与高德挂 AMap、百度挂 BMap 同理）
    script.onload = () => (window.L ? resolve(window.L) : reject(new Error('Leaflet: global missing')))
    script.onerror = () => reject(new Error('Leaflet: load failed'))
    document.head.appendChild(script)
  })
  return leafletSdk
}

/** 自绘落点图标：不走 Leaflet 默认的图片资产，免去 CDN 图标路径对不上的坑。 */
const PIN_HTML =
  '<svg width="26" height="34" viewBox="0 0 26 34" xmlns="http://www.w3.org/2000/svg">' +
  '<path d="M13 0C5.8 0 0 5.8 0 13c0 9.7 13 21 13 21s13-11.3 13-21C26 5.8 20.2 0 13 0z" fill="#ef4444"/>' +
  '<circle cx="13" cy="13" r="5" fill="#fff"/></svg>'

/** OpenStreetMap 选点：坐标 wgs84（与 GPS/谷歌一致）；检索与逆地理走后端 Nominatim 代理。 */
export function OsmPicker({ value, onChange, height = 420 }: MapPickerProps) {
  const { t } = useTranslation()
  const cfg = useMapsConfig()
  const pc = cfg.data?.osm
  const mapElRef = useRef<HTMLDivElement | null>(null)
  const mapRef = useRef<LeafletMap | null>(null)
  const markerRef = useRef<LeafletMarker | null>(null)
  const [sel, setSel] = useState<{ lng: number; lat: number } | null>(value ? { lng: value.lng, lat: value.lat } : null)
  const [address, setAddress] = useState(value?.address ?? '')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!pc?.enabled || !mapElRef.current) return
    let disposed = false
    let map: LeafletMap | null = null

    void (async () => {
      try {
        const L = await loadLeaflet()
        if (disposed || !mapElRef.current) return
        // 默认视野落北京（wgs84）；已有值时以值为中心
        const center: [number, number] = sel ? [sel.lat, sel.lng] : [39.9075, 116.3913]
        map = L.map(mapElRef.current, { zoom: 12, center })
        mapRef.current = map
        L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
          maxZoom: 19,
          attribution: '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
        }).addTo(map)
        const marker = L.marker(center, { draggable: true, icon: L.divIcon({ html: PIN_HTML, iconSize: [26, 34], iconAnchor: [13, 34], className: '' }) })
        markerRef.current = marker
        marker.addTo(map)
        marker.on('dragend', () => {
          const p = marker.getLatLng()
          setSel({ lng: p.lng, lat: p.lat })
          setAddress('')
        })
        map.on('click', (e) => {
          const { lng, lat } = e.latlng
          setSel({ lng, lat })
          setAddress('')
          markerRef.current?.setLatLng([lat, lng])
        })
      } catch (e) {
        if (!disposed) setError(e instanceof Error ? e.message : String(e))
      }
    })()

    return () => {
      disposed = true
      map?.remove()
      mapRef.current = null
      markerRef.current = null
    }
    // 只在启用开关变化时初始化一次；选点更新走 marker/state，不重建地图。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pc?.enabled])

  if (cfg.isLoading) return <p className="py-8 text-center text-sm text-muted-foreground">{t('common.loading')}</p>
  if (!pc?.enabled) {
    return <p className="py-8 text-center text-sm text-muted-foreground">{t('plugin_maps.osmDisabled')}</p>
  }

  const confirm = async () => {
    if (!sel) return
    setBusy(true)
    let addr = address
    try {
      const res = await regeo('osm', sel.lng, sel.lat)
      addr = res.address
      setAddress(addr)
    } catch {
      // 逆地理失败不阻塞选点，地址留给用户手填。
    } finally {
      setBusy(false)
    }
    onChange({ ...sel, system: 'wgs84', address: addr })
  }

  return (
    <div className="flex flex-col gap-2">
      {error ? <p className="text-xs text-destructive">{error.startsWith('Leaflet:') ? t('plugin_maps.osmLoadFailed') : error}</p> : null}
      <PickerSearch provider="osm" onPick={({ lng, lat, address: addr }) => {
        setSel({ lng, lat })
        setAddress(addr)
        markerRef.current?.setLatLng([lat, lng])
        // 搜索结果可能离当前视野很远：平移过去，别让用户自己找点
        mapRef.current?.setView([lat, lng], 15)
      }} />
      <div ref={mapElRef} style={{ height }} className="w-full overflow-hidden rounded-md border border-border bg-muted" />
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
        <span className="num">{sel ? `${sel.lng.toFixed(6)}, ${sel.lat.toFixed(6)}` : t('plugin_maps.clickHint')}</span>
        {address ? <span className="min-w-0 truncate">{address}</span> : null}
        <Button type="button" size="sm" disabled={!sel || busy} onClick={() => void confirm()}>
          {t('plugin_maps.confirm')}
        </Button>
      </div>
    </div>
  )
}
