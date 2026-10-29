import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import type { MapPickerProps } from '@/framework/map'
import { regeo, useMapsConfig } from './api'

// 百度 JS API GL 全局对象；只声明用到的部分。
type BMapGLNS = {
  Map: new (el: HTMLElement) => {
    centerAndZoom: (p: unknown, zoom: number) => void
    enableScrollWheelZoom: (v: boolean) => void
    addOverlay: (o: unknown) => void
    removeOverlay: (o: unknown) => void
    addEventListener: (ev: string, fn: (e: { latlng: { lng: number; lat: number } }) => void) => void
  }
  Point: new (lng: number, lat: number) => unknown
  Marker: new (p: unknown) => {
    enableDragging: () => void
    setPosition: (p: unknown) => void
    addEventListener: (ev: string, fn: () => void) => void
    getPosition: () => { lng: number; lat: number }
  }
}

declare global {
  interface Window {
    BMapGL?: BMapGLNS
  }
}

let sdk: Promise<BMapGLNS> | null = null
let sdkKey = ''

/** 百度 JS API GL：坐标系是 BD-09。 */
function loadBaidu(ak: string): Promise<BMapGLNS> {
  if (window.BMapGL && sdkKey === ak) return Promise.resolve(window.BMapGL)
  if (sdk && sdkKey === ak) return sdk
  sdkKey = ak
  sdk = new Promise<BMapGLNS>((resolve, reject) => {
    const script = document.createElement('script')
    script.async = true
    script.src = `https://api.map.baidu.com/api?type=webgl&v=3.0&ak=${encodeURIComponent(ak)}`
    script.onload = () => (window.BMapGL ? resolve(window.BMapGL) : reject(new Error('BMapGL: global missing')))
    script.onerror = () => reject(new Error('BMapGL: load failed'))
    document.head.appendChild(script)
  })
  return sdk
}

export function BaiduPicker({ value, onChange, height = 420 }: MapPickerProps) {
  const { t } = useTranslation()
  const cfg = useMapsConfig()
  const pc = cfg.data?.baidu
  const elRef = useRef<HTMLDivElement | null>(null)
  const [sel, setSel] = useState<{ lng: number; lat: number } | null>(value ? { lng: value.lng, lat: value.lat } : null)
  const [address, setAddress] = useState(value?.address ?? '')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const ak = pc?.publicKey
    if (!pc?.enabled || !ak || !elRef.current) return
    let disposed = false
    let map: InstanceType<BMapGLNS['Map']> | null = null
    let marker: InstanceType<BMapGLNS['Marker']> | null = null
    void (async () => {
      try {
        const BMapGL = await loadBaidu(ak)
        if (disposed || !elRef.current) return
        map = new BMapGL.Map(elRef.current)
        map.centerAndZoom(new BMapGL.Point(sel?.lng ?? 116.404, sel?.lat ?? 39.915), 12)
        map.enableScrollWheelZoom(true)
        marker = new BMapGL.Marker(new BMapGL.Point(sel?.lng ?? 116.404, sel?.lat ?? 39.915))
        marker.enableDragging()
        map.addOverlay(marker)
        marker.addEventListener('dragend', () => {
          if (!marker) return
          const p = marker.getPosition()
          setSel({ lng: p.lng, lat: p.lat })
          setAddress('')
        })
        map.addEventListener('click', (e) => {
          setSel({ lng: e.latlng.lng, lat: e.latlng.lat })
          setAddress('')
          marker?.setPosition(new BMapGL.Point(e.latlng.lng, e.latlng.lat))
        })
      } catch (e) {
        if (!disposed) setError(e instanceof Error ? e.message : String(e))
      }
    })()
    return () => {
      disposed = true
    }
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
      addr = (await regeo('baidu', sel.lng, sel.lat)).address
      setAddress(addr)
    } catch {
      // 逆地理失败不阻塞选点，地址留给用户手填。
    } finally {
      setBusy(false)
    }
    onChange({ ...sel, system: 'bd09', address: addr })
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
