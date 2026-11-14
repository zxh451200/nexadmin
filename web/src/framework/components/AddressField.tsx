import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { MapPin } from 'lucide-react'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { cn } from '@/lib/utils'
import { ResponsiveDialog } from './ResponsiveDialog'
import { RegionSelect } from './RegionSelect'
import { parseRegionAddress } from './pickers/parseRegion'
import { findRegionPath, useRegionTree, type RegionValue } from './pickers/useRegionTree'
import type { RegionNode } from '@/framework/request/types'
import { getMapProviders } from '@/framework/map'
import type { GeoPoint } from '@/framework/map'
import '@/framework/map/loader'

/** 地址复合值：region 是省市区编码，address 是详细地址（持久化的自由文本）。
    完整地址不落库，由区划树 + address 推导。 */
export interface AddressValue {
  region?: RegionValue
  address?: string
}

export interface AddressFieldProps {
  value?: AddressValue | null
  onChange?: (value: AddressValue) => void
  /** 区划层级，默认 3（省/市/区县）；4 = 加街道/乡镇，街道名折进详细地址开头（业务表只存 6 位代码） */
  level?: 2 | 3 | 4
  disabled?: boolean
  className?: string
}

function toRegionValue(path: RegionNode[]): RegionValue {
  return { province: path[0]?.code, city: path[1]?.code, district: path[2]?.code }
}

function sameRegion(a?: RegionValue, b?: RegionValue): boolean {
  return a?.province === b?.province && a?.city === b?.city && a?.district === b?.district
}

/** 最近使用的地址（本机 localStorage，隐私数据不出设备），最多保留 3 条。 */
interface RecentAddress {
  label: string
  region: RegionValue
  address: string
}

const RECENT_KEY = 'nex-recent-addresses'
const RECENT_MAX = 3

function loadRecent(): RecentAddress[] {
  try {
    const raw = JSON.parse(localStorage.getItem(RECENT_KEY) ?? '[]') as RecentAddress[]
    return Array.isArray(raw) ? raw.slice(0, RECENT_MAX) : []
  } catch {
    return []
  }
}

function labelOf(tree: RegionNode[], v: AddressValue): string {
  const names = findRegionPath(tree, v.region).map((n) => n.name).join('')
  return names + (v.address ?? '')
}

function saveRecent(tree: RegionNode[], v: AddressValue): RecentAddress[] {
  if (!v.region?.province || !(v.address ?? '').trim()) return loadRecent()
  const label = labelOf(tree, v)
  const next = [{ label, region: v.region, address: v.address ?? '' }, ...loadRecent().filter((r) => r.label !== label)].slice(0, RECENT_MAX)
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(next))
  } catch {
    // 隐私模式 / 存储满：最近地址是不可省数据，静默放弃即可
  }
  return next
}

/**
 * 顺丰/电商式地址字段：省市区选择 + 详细地址 + 完整地址三段联动，外加地图选点与最近使用。
 * - 往完整地址里输入（如「中牟散大声地111」）会**边打边解析**：区划选择器实时跟进（区县名认
 *   短别名“中牟”、曾用名“思茅”，不带省市时按全树唯一命中补全），详细地址等失焦时再回填，
 *   避免打字过程中输入框内容被推导值顶掉；粘贴一段完整地址立即解析回填；解析不命中时整串
 *   进详细地址，不丢内容。
 * - 装了地图插件（任一厂商已启用）时出现「地图选点」：选点确认后按逆地理地址回填省市区与
 *   详细地址，坐标不落库。
 * - 最近使用的地址记在本机 localStorage（最多 3 条），点击整条回填；「清空」一键清掉当前值。
 */
export function AddressField({ value, onChange, level = 3, disabled, className }: AddressFieldProps) {
  const { t } = useTranslation()
  const { data: tree = [] } = useRegionTree(level)
  // 完整地址输入框的本地草稿：null = 跟随推导值；用户手动编辑后暂存，失焦/粘贴时解析回填
  const [fullDraft, setFullDraft] = useState<string | null>(null)
  const [recent, setRecent] = useState<RecentAddress[]>(() => loadRecent())
  // 地图选点弹窗：provider 列表来自注册表（地图插件加载后非空）
  const [mapOpen, setMapOpen] = useState(false)
  const providers = getMapProviders()
  const [providerId, setProviderId] = useState(providers[0]?.id ?? '')
  const activeProvider = providers.find((p) => p.id === providerId) ?? providers[0]
  // 自己 emit 出去的值（打字联动时 input 受控内容不能被推导值顶掉）；
  // 「外部改动」（区划选择、详细地址输入）与「完整地址打字引发的联动」用它区分。
  const emitted = useRef(value ?? null)
  const fromFull = useRef(false)

  const region = value?.region
  const address = value?.address ?? ''
  const fullComputed = findRegionPath(tree, region).map((n) => n.name).join('') + address

  useEffect(() => {
    const prev = emitted.current
    const changed =
      !sameRegion(value?.region, prev?.region) || (value?.address ?? '') !== (prev?.address ?? '')
    if (changed) {
      if (!fromFull.current) setFullDraft(null) // 外部改动：完整地址回到推导值
      fromFull.current = false
      emitted.current = value ?? null
    }
  // 只盯这几个字段：完整地址打字联动引发的 region/address 变化不把草稿顶掉（emitted 已记账），
  // eslint 覆盖依赖数这里是有意为之
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value?.region?.province, value?.region?.city, value?.region?.district, value?.address])

  const emit = (v: AddressValue, fromFullInput = false) => {
    fromFull.current = fromFullInput
    emitted.current = v
    onChange?.({ region: v.region, address: v.address })
  }

  // RegionSelect 在 level=4 会带出街道代码：街道名折进详细地址开头，region 只保留三级代码
  //（完整地址推导时街道已包含在 address 文本里，不会重复出现）
  const foldStreet = (v: AddressValue): AddressValue => {
    const path = findRegionPath(tree, v.region)
    if (path.length < 4) return v
    const st = path[3].name
    const addr = v.address ?? ''
    return { region: { province: path[0].code, city: path[1].code, district: path[2].code }, address: addr.startsWith(st) ? addr : st + addr }
  }

  // 解析结果的装配：level=4 时第 4 级可能是街道——街道名同样折进详细地址开头
  const toAddressValue = (path: RegionNode[], rest: string, fallback?: RegionValue): AddressValue => {
    if (!path.length) return { region: fallback, address: rest }
    if (path.length >= 4) return { region: toRegionValue(path), address: path[3].name + rest }
    return { region: toRegionValue(path), address: rest }
  }

  const applyParsed = (text: string) => {
    const clean = text.trim()
    if (!clean) return
    const { path, rest } = parseRegionAddress(tree, clean, level)
    const next = toAddressValue(path, rest, region)
    emit(next)
    setFullDraft(null)
    setRecent(saveRecent(tree, next))
  }

  // 打字过程的实时反馈：只把区划选择器跟上去，详细地址留到失焦再回填
  const liveParseRegion = (text: string) => {
    const { path } = parseRegionAddress(tree, text.trim(), level)
    if (path.length && !sameRegion(toRegionValue(path), region)) {
      emit({ region: toRegionValue(path), address }, true)
    }
  }

  const pickRecent = (r: RecentAddress) => {
    emit({ region: r.region, address: r.address })
    setFullDraft(null)
  }

  // 地图选点确认：把逆地理地址解析回省市区 + 详细地址（解析不动就整串进详细地址）
  const applyPoint = (p: GeoPoint) => {
    const text = (p.address ?? '').trim()
    const { path, rest } = text ? parseRegionAddress(tree, text, level) : { path: [] as RegionNode[], rest: text }
    const v = toAddressValue(path, rest, region)
    emit(path.length ? v : { region: region, address: rest || text })
    setMapOpen(false)
    setFullDraft(null)
  }

  return (
    <div className={cn('flex flex-col gap-3', className)}>
      {recent.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
          <span className="shrink-0">{t('picker.recent')}</span>
          {recent.map((r) => (
            <button
              key={r.label}
              type="button"
              disabled={disabled}
              onClick={() => pickRecent(r)}
              className="max-w-72 truncate rounded-full border border-border px-2.5 py-1 text-xs hover:bg-muted focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
              title={r.label}
            >
              {r.label}
            </button>
          ))}
        </div>
      )}
      <RegionSelect
        level={level}
        value={region ?? null}
        disabled={disabled}
        onChange={(r) => {
          const v = foldStreet({ region: r, address })
          emit(v)
          setRecent(saveRecent(tree, v))
        }}
      />
      <label className="flex flex-col gap-1 text-xs text-muted-foreground">
        {t('picker.addressDetail')}
        <Input
          value={address}
          disabled={disabled}
          maxLength={255}
          onChange={(e) => emit({ region, address: e.target.value })}
          onBlur={() => setRecent(saveRecent(tree, { region, address }))}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-muted-foreground">
        {t('picker.addressFull')}
        <Input
          value={fullDraft ?? fullComputed}
          disabled={disabled}
          maxLength={512}
          placeholder={t('picker.addressPasteHint')}
          onChange={(e) => {
            const text = e.target.value
            setFullDraft(text)
            liveParseRegion(text)
          }}
          onPaste={(e) => {
            const text = e.clipboardData.getData('text')
            if (!text.trim()) return
            e.preventDefault()
            applyParsed(text)
          }}
          onBlur={() => {
            if (fullDraft != null) applyParsed(fullDraft)
          }}
        />
      </label>
      <div className="flex items-center justify-between gap-2">
        {providers.length > 0 ? (
          <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={() => setMapOpen(true)}>
            <MapPin aria-hidden="true" />
            {t('picker.mapPick')}
          </Button>
        ) : (
          <span />
        )}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={disabled || (!region?.province && !address)}
          onClick={() => emit({ region: {}, address: '' })}
        >
          {t('picker.clear')}
        </Button>
      </div>

      <ResponsiveDialog
        open={mapOpen}
        onOpenChange={setMapOpen}
        title={t('picker.mapPick')}
        size="lg"
        footer={
          <Button type="button" variant="outline" onClick={() => setMapOpen(false)}>
            {t('common.close')}
          </Button>
        }
      >
        {activeProvider ? (
          <>
            {providers.length > 1 ? (
              <Select value={activeProvider.id} onValueChange={setProviderId}>
                <SelectTrigger className="sm:w-56" aria-label={t('picker.mapPick')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {providers.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {t(`plugin_maps.providers.${p.id}`, { defaultValue: p.title })}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}
            {(() => {
              const Picker = activeProvider.Picker
              return <Picker height={380} onChange={applyPoint} />
            })()}
          </>
        ) : (
          <p className="py-8 text-center text-sm text-muted-foreground">{t('common.empty')}</p>
        )}
      </ResponsiveDialog>
    </div>
  )
}
