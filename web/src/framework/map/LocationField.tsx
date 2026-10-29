import { useState } from 'react'
import { MapPin } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import './loader'
import { getMapProviders } from './registry'
import type { GeoPoint } from './types'

export interface LocationFieldProps {
  value?: GeoPoint | null
  onChange: (p: GeoPoint) => void
  label?: React.ReactNode
  addressLabel?: React.ReactNode
  disabled?: boolean
  className?: string
  /** 选点地图高度 */
  height?: number
}

/** 经纬度 + 地址的表单字段；装了地图插件时可直接在地图上选点，没装就手填。 */
export function LocationField({ value, onChange, label, addressLabel, disabled, className, height }: LocationFieldProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const providers = getMapProviders()
  const [providerId, setProviderId] = useState(providers[0]?.id ?? '')
  const active = providers.find((p) => p.id === providerId) ?? providers[0]
  const point = value ?? null

  const setCoord = (key: 'lng' | 'lat', raw: string) => {
    const n = Number(raw)
    if (raw.trim() === '' || Number.isNaN(n)) {
      onChange({ ...(point ?? { lng: 0, lat: 0 }), [key]: 0 })
      return
    }
    onChange({ ...(point ?? { lng: 0, lat: 0 }), [key]: n })
  }

  return (
    <div className={className}>
      {label ? <Label className="mb-1.5 block">{label}</Label> : null}
      <div className="flex flex-col gap-2">
        <div className="flex flex-col gap-2 sm:flex-row">
          <div className="flex flex-1 flex-col gap-1.5">
            <Label htmlFor="geo-lng" className="text-xs text-muted-foreground">
              {t('map.lng')}
            </Label>
            <Input
              id="geo-lng"
              inputMode="decimal"
              className="num"
              disabled={disabled}
              value={point?.lng ? String(point.lng) : ''}
              onChange={(e) => setCoord('lng', e.target.value)}
              placeholder="116.397428"
            />
          </div>
          <div className="flex flex-1 flex-col gap-1.5">
            <Label htmlFor="geo-lat" className="text-xs text-muted-foreground">
              {t('map.lat')}
            </Label>
            <Input
              id="geo-lat"
              inputMode="decimal"
              className="num"
              disabled={disabled}
              value={point?.lat ? String(point.lat) : ''}
              onChange={(e) => setCoord('lat', e.target.value)}
              placeholder="39.90923"
            />
          </div>
          <div className="flex items-end">
            <Button type="button" variant="outline" disabled={disabled || providers.length === 0} onClick={() => setOpen(true)}>
              <MapPin aria-hidden="true" />
              {t('map.pick')}
            </Button>
          </div>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="geo-address" className="text-xs text-muted-foreground">
            {addressLabel ?? t('map.address')}
          </Label>
          <Input
            id="geo-address"
            disabled={disabled}
            value={point?.address ?? ''}
            onChange={(e) => onChange({ ...(point ?? { lng: 0, lat: 0 }), address: e.target.value })}
          />
        </div>
        {providers.length === 0 ? <p className="text-xs text-muted-foreground">{t('map.noProvider')}</p> : null}
      </div>

      <ResponsiveDialog
        open={open}
        onOpenChange={setOpen}
        title={t('map.pickTitle')}
        description={providers.map((p) => p.title).join(' / ')}
        size="lg"
        footer={
          <Button type="button" variant="outline" onClick={() => setOpen(false)}>
            {t('common.close')}
          </Button>
        }
      >
        {providers.length > 0 ? (
          <div className="flex flex-col gap-3">
            {providers.length > 1 ? (
              <Select value={active?.id} onValueChange={setProviderId}>
                <SelectTrigger className="sm:w-56" aria-label={t('map.provider')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {providers.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.title}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}
            {(() => {
              if (!active) return null
              const Picker = active.Picker
              return (
                <Picker
                  height={height ?? 420}
                  value={point}
                  onChange={(p) => {
                    onChange(p)
                    setOpen(false)
                  }}
                />
              )
            })()}
          </div>
        ) : (
          <p className="py-8 text-center text-sm text-muted-foreground">{t('map.noProvider')}</p>
        )}
      </ResponsiveDialog>
    </div>
  )
}
