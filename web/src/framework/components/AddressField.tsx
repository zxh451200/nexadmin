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

/** 地址复合值：region 是省市区三级编码（街道不存码，行业主流做法），address 是持久化的
    自由文本——level=4 时选中的街道名会折在 address 开头。完整地址不落库，由区划树 + address 推导。 */
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

/** 街道不单独存储（行业主流：省市区三级代码 + 详细地址文本，街道写进文本）：
    四级省份选完街道会把街道名折进详细地址开头。这里按区县子级的名称前缀找出已折叠的
    街道名，供「清除地区时连街道一起剥掉」「换选街道时替换前缀」使用。
    直辖市不走这条路径：末位街道的码存在 region 的 district 槽位里，显示由区划树带出。 */
function splitStreet(tree: RegionNode[], region: RegionValue | undefined, address: string): string | undefined {
  const district = findRegionPath(tree, region)[2]
  return (district?.children ?? [])
    .filter((c) => address.startsWith(c.name))
    .sort((a, b) => b.name.length - a.name.length)[0]?.name
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
 * - level=4 的第四级街道/乡镇不存码（行业主流：省市区三级代码，街道写进详细地址文本）：
 *   选完街道街道名折进详细地址开头；清除地区会连街道名一起剥掉（用户输入的正文保留），
 *   换选街道会替换旧街道前缀，不残留。
 * - 往完整地址里输入（如「中牟散大声地111」）会**边打边解析**：区划选择器与详细地址实时跟进
 *   （区县名认短别名“中牟”、曾用名“思茅”，不带省市时按全树唯一命中补全；完整地址输入框自身
 *   有草稿保护，联动不会顶掉正在输入的内容）；粘贴一段完整地址立即解析回填；解析不命中时整串
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

  // 解析结果的装配：level=4 时第 4 级可能是街道——街道名折进详细地址开头。
  // 只认 path.length>=4：直辖市路径三级里末位就是街道，但那个街道码已经存在 region 的
  // district 槽位里（完整地址由区划树推导时自然带出），再折进正文会出现两次。
  const toAddressValue = (path: RegionNode[], rest: string, fallback?: RegionValue): AddressValue => {
    if (!path.length) return { region: fallback, address: rest }
    if (path.length >= 4) return { region: toRegionValue(path), address: path[3].name + rest }
    return { region: toRegionValue(path), address: rest }
  }

  const applyParsed = (text: string) => {
    const clean = text.trim()
    if (!clean) {
      // 整段删空 = 连地区带正文一起清掉（否则完整地址看起来删了，底下还留着旧值）
      if (region?.province || address) emit({ region: {}, address: '' })
      return
    }
    const { path, rest } = parseRegionAddress(tree, clean, level)
    const next = toAddressValue(path, rest, region)
    emit(next)
    setFullDraft(null)
    setRecent(saveRecent(tree, next))
  }

  // 打字过程的实时反馈：区划选择器与详细地址同步跟进。完整地址输入框自身有
  // fullDraft 草稿保护，联动不会顶掉正在输入的内容；解析不命中（删到没有区划
  // 前缀）时不动当前值，整串进详细地址留给失焦时兜底
  const liveParseRegion = (text: string) => {
    const { path, rest } = parseRegionAddress(tree, text.trim(), level)
    if (!path.length) return
    const next = toAddressValue(path, rest, region)
    if (!sameRegion(next.region, region) || (next.address ?? '') !== address) {
      emit(next, true)
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
          // 街道不存码（行业主流：省市区三级代码，街道写进详细地址文本）
          if (!r.province) {
            // 清除地区：连折叠进详细地址开头的街道名一起剥掉，用户输入的正文保留
            const old = splitStreet(tree, region, address)
            const rest = old && address.startsWith(old) ? address.slice(old.length) : address
            emit({ region: r, address: rest })
            setRecent(saveRecent(tree, { region: r, address: rest }))
            return
          }
          const path = findRegionPath(tree, r)
          const street = path[3]?.name
          if (!street) {
            // 只到三级就收口（直辖市末位街道的码已存进 region、显示由区划树带出；
            // 搜索结果/旧数据可能没有街道）：没有需要折进正文的街道，地址原样保留
            const v = { region: toRegionValue(path), address }
            emit(v)
            setRecent(saveRecent(tree, v))
            return
          }
          // 四级省份换选街道：先剥掉旧街道前缀再折进新街道，避免旧街道残留在正文前（实测会叠出双街道）
          const old = splitStreet(tree, region, address)
          const rest = old && address.startsWith(old) ? address.slice(old.length) : address
          const v = { region: toRegionValue(path), address: street + rest }
          emit(v)
          setRecent(saveRecent(tree, v))
        }}
      />
      {/* label 文本的可复制性由 framework/lib/label-copy 的全局双击选中原语统一保证 */}
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
