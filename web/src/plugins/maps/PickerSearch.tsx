import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { MapPin, Search } from 'lucide-react'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { searchPlaces, type MapPlace, type MapProviderId } from './api'

/**
 * 选点地图的关键字搜索框：关键词 → 后端 POI 代理 → 结果列表。
 * 三家 picker 共用；坐标是厂商原生坐标系，直接落到同厂商地图上。
 */
export function PickerSearch({
  provider,
  onPick,
}: {
  provider: MapProviderId
  onPick: (p: { lng: number; lat: number; address: string }) => void
}) {
  const { t } = useTranslation()
  const [keywords, setKeywords] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [results, setResults] = useState<MapPlace[] | null>(null)

  const search = async () => {
    const kw = keywords.trim()
    if (!kw || busy) return
    setBusy(true)
    setError(null)
    try {
      const rows = await searchPlaces(provider, kw)
      setResults(rows)
      if (rows.length === 0) setError(t('plugin_maps.searchNoResults'))
    } catch (e) {
      setResults(null)
      setError(e instanceof Error && e.message ? e.message : t('plugin_maps.searchFailed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-1.5">
      <form
        className="flex gap-1.5"
        onSubmit={(e) => {
          e.preventDefault()
          void search()
        }}
      >
        <Input
          value={keywords}
          onChange={(e) => setKeywords(e.target.value)}
          placeholder={t('plugin_maps.searchPlaceholder')}
          aria-label={t('plugin_maps.searchPlaceholder')}
          className="h-9"
        />
        <Button type="submit" variant="outline" size="icon" className="size-9 shrink-0" disabled={busy} aria-label={t('plugin_maps.search')}>
          <Search aria-hidden="true" className="size-4" />
        </Button>
      </form>
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
      {results && results.length > 0 ? (
        <ul className="max-h-40 space-y-1 overflow-y-auto rounded-md border border-border p-1">
          {results.map((r, i) => (
            <li key={`${r.lng}-${r.lat}-${i}`}>
              <button
                type="button"
                onClick={() => onPick({ lng: r.lng, lat: r.lat, address: r.address ? `${r.name}（${r.address}）` : r.name })}
                className="flex w-full items-start gap-2 rounded px-2 py-1.5 text-start text-xs hover:bg-accent"
              >
                <MapPin aria-hidden="true" className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium text-foreground">{r.name}</span>
                  {r.address ? <span className="block truncate text-muted-foreground">{r.address}</span> : null}
                </span>
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}
