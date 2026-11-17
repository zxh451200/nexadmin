import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import type { MapPickerProps } from '@/framework/map'
import { regeo, useMapsConfig } from './api'
import { PickerSearch } from './PickerSearch'

// 天地图 JS API 4.0 全局对象 T；只声明用到的部分。
type TLngLat = { getLng: () => number; getLat: () => number }
type TMapNS = {
  Map: new (el: HTMLElement) => {
    centerAndZoom: (p: unknown, zoom: number) => void
    enableScrollWheelZoom: () => void
    // 底图切换：传全局常量 TMAP_NORMAL_MAP（矢量）/ TMAP_SATELLITE_MAP（影像）
    setMapType: (t: unknown) => void
    addOverLay: (o: unknown) => void
    removeOverLay: (o: unknown) => void
    addEventListener: (ev: string, fn: (e: { lnglat: TLngLat }) => void) => void
  }
  LngLat: new (lng: number, lat: number) => TLngLat
  Marker: new (p: unknown) => {
    enableDragging: () => void
    setLngLat: (p: unknown) => void
    addEventListener: (ev: string, fn: () => void) => void
    getLngLat: () => TLngLat
  }
}

declare global {
  interface Window {
    T?: TMapNS
    // 底图类型常量：SDK 加载后挂在 window 上（官方 JS API 4.0 约定）
    TMAP_NORMAL_MAP?: unknown
    TMAP_SATELLITE_MAP?: unknown
  }
}

let sdk: Promise<TMapNS> | null = null
let sdkKey = ''

/** 天地图 JS API：坐标系 CGCS2000（与 WGS-84 厘米级重合，按 wgs84 落点）。 */
function loadTianditu(tk: string): Promise<TMapNS> {
  if (window.T && sdkKey === tk) return Promise.resolve(window.T)
  if (sdk && sdkKey === tk) return sdk
  sdkKey = tk
  sdk = new Promise<TMapNS>((resolve, reject) => {
    const script = document.createElement('script')
    script.async = true
    script.src = `https://api.tianditu.gov.cn/api?v=4.0&tk=${encodeURIComponent(tk)}`
    script.onload = () => (window.T ? resolve(window.T) : reject(new Error('tianditu-global-missing')))
    script.onerror = () => reject(new Error('tianditu-load-failed'))
    document.head.appendChild(script)
  })
  return sdk
}

export function TiandituPicker({ value, onChange, height = 420 }: MapPickerProps) {
  const { t } = useTranslation()
  const cfg = useMapsConfig()
  const pc = cfg.data?.tianditu
  const elRef = useRef<HTMLDivElement | null>(null)
  // markerRef / nsRef：搜索选中时把标记挪过去（marker 与 T 命名空间都在 effect 里创建）
  const markerRef = useRef<{ setLngLat: (p: unknown) => void } | null>(null)
  const nsRef = useRef<TMapNS | null>(null)
  const mapRef = useRef<{ setMapType: (t: unknown) => void } | null>(null)
  // 底图类型：vec 矢量 / img 影像；ref 供 effect 重建地图时还原
  const [base, setBase] = useState<'vec' | 'img'>('vec')
  const baseRef = useRef<'vec' | 'img'>('vec')
  const [sel, setSel] = useState<{ lng: number; lat: number } | null>(value ? { lng: value.lng, lat: value.lat } : null)
  const [address, setAddress] = useState(value?.address ?? '')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const tk = pc?.publicKey
    if (!pc?.enabled || !tk || !elRef.current) return
    let disposed = false
    let map: InstanceType<TMapNS['Map']> | null = null
    let marker: InstanceType<TMapNS['Marker']> | null = null
    void (async () => {
      try {
        const T = await loadTianditu(tk)
        nsRef.current = T
        if (disposed || !elRef.current) return
        map = new T.Map(elRef.current)
        map.centerAndZoom(new T.LngLat(sel?.lng ?? 116.397428, sel?.lat ?? 39.90923), 12)
        map.enableScrollWheelZoom()
        mapRef.current = map
        if (baseRef.current === 'img' && window.TMAP_SATELLITE_MAP !== undefined) {
          map.setMapType(window.TMAP_SATELLITE_MAP)
        }
        marker = new T.Marker(new T.LngLat(sel?.lng ?? 116.397428, sel?.lat ?? 39.90923))
        marker.enableDragging()
        markerRef.current = marker
        map.addOverLay(marker)
        marker.addEventListener('dragend', () => {
          if (!marker) return
          const p = marker.getLngLat()
          setSel({ lng: p.getLng(), lat: p.getLat() })
          setAddress('')
        })
        map.addEventListener('click', (e) => {
          const lng = e.lnglat.getLng()
          const lat = e.lnglat.getLat()
          setSel({ lng, lat })
          setAddress('')
          marker?.setLngLat(new T.LngLat(lng, lat))
        })
      } catch (e) {
        if (!disposed) setError(e instanceof Error ? e.message : String(e))
      }
    })()
    return () => {
      disposed = true
    }
    // 只在配置到达时初始化一次；选点更新走 marker/state，不重建地图。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pc?.enabled, pc?.publicKey])

  if (cfg.isLoading) return <p className="py-8 text-center text-sm text-muted-foreground">{t('common.loading')}</p>
  if (!pc?.enabled || !pc.publicKey) {
    return <p className="py-8 text-center text-sm text-muted-foreground">{t('plugin_maps.notConfigured')}</p>
  }

  // 矢量 / 影像底图切换：SDK 已加载后直接换 TMAP 常量；地图未就绪时只记状态，重建时还原。
  const switchBase = (b: 'vec' | 'img') => {
    setBase(b)
    baseRef.current = b
    const t = b === 'img' ? window.TMAP_SATELLITE_MAP : window.TMAP_NORMAL_MAP
    if (t !== undefined) mapRef.current?.setMapType(t)
  }

  const confirm = async () => {
    if (!sel) return
    setBusy(true)
    let addr = address
    try {
      addr = (await regeo('tianditu', sel.lng, sel.lat)).address
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
      {error ? (
        <p className="text-xs text-destructive">
          {error.startsWith('tianditu-') ? t('plugin_maps.tiandituLoadFailed') : error}
        </p>
      ) : null}
      <PickerSearch provider="tianditu" onPick={({ lng, lat, address }) => {
        setSel({ lng, lat })
        setAddress(address)
        const ns = nsRef.current
        if (ns) markerRef.current?.setLngLat(new ns.LngLat(lng, lat))
      }} />
      <div className="flex items-center gap-1">
        <Button type="button" size="sm" variant={base === 'vec' ? 'secondary' : 'ghost'} onClick={() => switchBase('vec')}>
          {t('plugin_maps.baseVec')}
        </Button>
        <Button type="button" size="sm" variant={base === 'img' ? 'secondary' : 'ghost'} onClick={() => switchBase('img')}>
          {t('plugin_maps.baseImg')}
        </Button>
      </div>
      <div ref={elRef} style={{ height }} className="w-full overflow-hidden rounded-md border border-border bg-muted" />
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
