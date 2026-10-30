import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import type { MapPickerProps } from '@/framework/map'
import { regeo, useMapsConfig } from './api'

type LatLng = { lat: () => number; lng: () => number }
type GMap = {
  addListener: (ev: string, fn: (e: { latLng: LatLng }) => void) => void
}
type GMarker = {
  setPosition: (p: { lat: number; lng: number }) => void
  addListener: (ev: string, fn: () => void) => void
  getPosition: () => LatLng
}
type GoogleNS = {
  maps: {
    Map: new (el: HTMLElement, opts: Record<string, unknown>) => GMap
    Marker: new (opts: Record<string, unknown>) => GMarker
  }
}

declare global {
  interface Window {
    google?: GoogleNS
  }
}

let sdk: Promise<GoogleNS> | null = null
let sdkKey = ''

/** Google Maps JS API：坐标系是 WGS-84（与 OSM/GPS 一致）。 */
function loadGoogle(key: string, lang: string): Promise<GoogleNS> {
  if (window.google?.maps && sdkKey === key) return Promise.resolve(window.google)
  if (sdk && sdkKey === key) return sdk
  sdkKey = key
  sdk = new Promise<GoogleNS>((resolve, reject) => {
    const script = document.createElement('script')
    script.async = true
    script.src = `https://maps.googleapis.com/maps/api/js?key=${encodeURIComponent(key)}&v=weekly&language=${encodeURIComponent(lang)}`
    script.onload = () => (window.google?.maps ? resolve(window.google) : reject(new Error('google-global-missing')))
    script.onerror = () => reject(new Error('google-load-failed'))
    document.head.appendChild(script)
  })
  return sdk
}

export function GooglePicker({ value, onChange, height = 420 }: MapPickerProps) {
  const { t, i18n } = useTranslation()
  const cfg = useMapsConfig()
  const pc = cfg.data?.google
  const elRef = useRef<HTMLDivElement | null>(null)
  const [sel, setSel] = useState<{ lng: number; lat: number } | null>(value ? { lng: value.lng, lat: value.lat } : null)
  const [address, setAddress] = useState(value?.address ?? '')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    const key = pc?.publicKey
    if (!pc?.enabled || !key || !elRef.current) return
    let disposed = false
    void (async () => {
      try {
        const g = await loadGoogle(key, i18n.language.startsWith('zh') ? 'zh-CN' : 'en')
        if (disposed || !elRef.current) return
        const center = { lat: sel?.lat ?? 39.90923, lng: sel?.lng ?? 116.397428 }
        const map = new g.maps.Map(elRef.current, { center, zoom: 12 })
        const marker = new g.maps.Marker({ position: center, map, draggable: true })
        marker.addListener('dragend', () => {
          const p = marker.getPosition()
          setSel({ lng: p.lng(), lat: p.lat() })
          setAddress('')
        })
        map.addListener('click', (e) => {
          const lng = e.latLng.lng()
          const lat = e.latLng.lat()
          setSel({ lng, lat })
          setAddress('')
          marker.setPosition({ lat, lng })
        })
      } catch (e) {
        if (disposed) return
        const code = e instanceof Error ? e.message : ''
        if (code === 'google-load-failed') setError(t('plugin_maps.googleLoadFailed'))
        else if (code === 'google-global-missing') setError(t('plugin_maps.googleGlobalMissing'))
        else setError(String(e))
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
      addr = (await regeo('google', sel.lng, sel.lat)).address
      setAddress(addr)
    } catch {
      // 逆地理失败不阻塞选点。
    } finally {
      setBusy(false)
    }
    onChange({ ...sel, system: 'wgs84', address: addr })
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
