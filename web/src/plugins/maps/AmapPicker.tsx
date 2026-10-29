import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import type { MapPickerProps } from '@/framework/map'
import { regeo, useMapsConfig } from './api'

// 高德 JS API 全局对象；只声明用到的部分。
type AMapNS = {
  Map: new (el: HTMLElement, opts: Record<string, unknown>) => {
    add: (o: unknown) => void
    remove: (o: unknown) => void
    on: (ev: string, fn: (e: { lnglat: { getLng: () => number; getLat: () => number } }) => void) => void
  }
  Marker: new (opts: Record<string, unknown>) => {
    setPosition: (p: [number, number]) => void
    on: (ev: string, fn: () => void) => void
    getPosition: () => { getLng: () => number; getLat: () => number }
  }
}

declare global {
  interface Window {
    AMap?: AMapNS
  }
}

let sdk: Promise<AMapNS> | null = null
let sdkKey = ''

/** 按 Key 加载一次高德 JS SDK（换 Key 会重新加载）。 */
function loadAmap(key: string): Promise<AMapNS> {
  if (window.AMap && sdkKey === key) return Promise.resolve(window.AMap)
  if (sdk && sdkKey === key) return sdk
  sdkKey = key
  sdk = new Promise<AMapNS>((resolve, reject) => {
    const script = document.createElement('script')
    script.async = true
    script.src = `https://webapi.amap.com/maps?v=2.0&key=${encodeURIComponent(key)}`
    script.onload = () => (window.AMap ? resolve(window.AMap) : reject(new Error('AMap: global missing')))
    script.onerror = () => reject(new Error('AMap: load failed'))
    document.head.appendChild(script)
  })
  return sdk
}

/** 高德选点：坐标是 GCJ-02；确认时把坐标与逆地理地址一起交回。 */
export function AmapPicker({ value, onChange, height = 420 }: MapPickerProps) {
  const { t } = useTranslation()
  const cfg = useMapsConfig()
  const pc = cfg.data?.amap
  const elRef = useRef<HTMLDivElement | null>(null)
  const [sel, setSel] = useState<{ lng: number; lat: number } | null>(value ? { lng: value.lng, lat: value.lat } : null)
  const [address, setAddress] = useState(value?.address ?? '')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const key = pc?.publicKey
    if (!pc?.enabled || !key || !elRef.current) return
    let disposed = false
    let map: InstanceType<AMapNS['Map']> | null = null
    let marker: InstanceType<AMapNS['Marker']> | null = null

    void (async () => {
      try {
        const AMap = await loadAmap(key)
        if (disposed || !elRef.current) return
        const center: [number, number] = sel ? [sel.lng, sel.lat] : [116.397428, 39.90923]
        map = new AMap.Map(elRef.current, { zoom: 12, center })
        marker = new AMap.Marker({ position: center, draggable: true })
        map.add(marker)
        marker.on('dragend', () => {
          if (!marker) return
          const p = marker.getPosition()
          setSel({ lng: p.getLng(), lat: p.getLat() })
          setAddress('')
        })
        map.on('click', (e) => {
          const lng = e.lnglat.getLng()
          const lat = e.lnglat.getLat()
          setSel({ lng, lat })
          setAddress('')
          marker?.setPosition([lng, lat])
        })
      } catch (e) {
        if (!disposed) setError(e instanceof Error ? e.message : String(e))
      }
    })()

    return () => {
      disposed = true
      map?.remove(marker as unknown as object)
    }
    // 只在配置到达时初始化一次；选点更新走 marker/state，不重建地图。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pc?.enabled, pc?.publicKey])

  if (cfg.isLoading) return <p className="py-8 text-center text-sm text-muted-foreground">{t('common.loading')}</p>
  if (!pc?.enabled || !pc.publicKey) {
    return <p className="py-8 text-center text-sm text-muted-foreground">{t('plugin_maps.notConfigured')}</p>
  }

  const confirm = async () => {
    if (!sel) return
    setBusy(true)
    let addr = address
    try {
      const res = await regeo('amap', sel.lng, sel.lat)
      addr = res.address
      setAddress(addr)
    } catch {
      // 逆地理失败不阻塞选点，地址留给用户手填。
    } finally {
      setBusy(false)
    }
    onChange({ ...sel, system: 'gcj02', address: addr })
  }

  return (
    <div className="flex flex-col gap-2">
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
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
