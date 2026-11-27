import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { ExternalLink, GripVertical, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { DndContext, type DragEndEvent } from '@dnd-kit/core'
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { DragCursorHost, pointerFirstCollision, useSortableSensors } from '@/framework/components/table/use-sortable-sensors'
import {
  LocaleFormActions,
  LocaleFormProvider,
  LocalizedField,
  LocaleSwitcher,
  SharedFieldShell,
  useLocaleForm,
  useSharedField,
  type TranslateBatch,
} from '@/framework/components/i18n'
import { FieldHint } from '@/framework/components/FieldHint'
import { ConstantPicker } from '@/framework/components/ConstantPicker'
import { LocationField } from '@/framework/map/LocationField'
import '@/plugins/maps/map-provider'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { ImageUrlField } from '@/framework/components/ImageUrlField'
import { SwitchRow } from '@/framework/components/SwitchField'
import { Skeleton } from '@/framework/ui/skeleton'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import { cn } from '@/lib/utils'
import {
  translateContent,
  useI18nSchema,
  useLanguages,
  usePages,
  useThemes,
  useSeedThemeDemo,
  useWebsiteLimits,
  useSaveSettings,
  useFetchCurrencyRates,
  useSettings,
  type FriendLink,
  type WebsiteLimits,
  type SiteSettingsForm,
  type SocialLink,
  type SocialPlatform,
  type WebsiteOffice,
} from './api'
import { ThemeCapNote, useThemeCap } from './ThemeCapNotice'

/** 不随语言变化的字段：留在页面本地 state，译文态下置灰。 */
type SharedTextKey =
  | 'siteName' | 'siteOrigin' | 'logoUrl' | 'faviconUrl' | 'ogImageUrl' | 'headCode' | 'titleSuffix' | 'homeSlug' | 'homeCtaHref' | 'contactEmail'
  | 'messageRecipientEmail' | 'heroImageUrl' | 'heroImageDarkUrl' | 'robotsTxt'
  | 'contactPhone' | 'qrCodeUrl' | 'qrCodeCaption' | 'icp' | 'contactConsentUrl' | 'theme' | 'defaultLang'
  | 'defaultCurrency' | 'currencyRates'
  | 'staffCount' | 'annualRevenue' | 'mainMarkets' | 'responseTime'
type SharedSettings = Pick<SiteSettingsForm, SharedTextKey | 'marketEnabled' | 'purchaseEnabled' | 'chatEnabled' | 'messageNotifyEmail' | 'contactConsent' | 'establishedYear'> & {
  socials: SocialLink[]
  friendLinks: FriendLink[]
  offices: WebsiteOffice[]
}

/** 社交平台下拉的顺序与文案；值集合与后端 socialPlatforms 一致。 */
const SOCIAL_PLATFORMS: SocialPlatform[] = [
  'github',
  'x',
  'linkedin',
  'youtube',
  'instagram',
  'facebook',
  'discord',
  'telegram',
  'whatsapp',
  'reddit',
  'tiktok',
  'twitch',
  'mastodon',
  'wechat',
  'weibo',
  'bilibili',
  'rss',
  'mail',
]

/** 可翻译字段：归 LocaleFormProvider，与后端 setting schema 同源。 */
type LocalizedKey = 'siteDesc' | 'homeTitle' | 'homeSubtitle' | 'homeCtaLabel' | 'seoKeywords' | 'footerNote'

/** 汇率表 JSON 的键集合（默认币种下拉的选项源）：USD 置顶，其余按码排序；
    解析失败返回空数组——前端不拦，真正的结构校验在后端保存时做。 */
function currencyRatesKeys(raw: string): string[] {
  try {
    const obj = JSON.parse(raw) as Record<string, number>
    return Object.keys(obj).sort((a, b) => (a === 'USD' ? -1 : b === 'USD' ? 1 : a.localeCompare(b)))
  } catch {
    return []
  }
}

/** 汇率行：编辑态把 JSON 文本摊成「币种码 + 兑美元汇率」两列，保存时再拼回去。 */
interface RateRow {
  code: string
  rate: string
}

/** 宽松解析：JSON 坏了当空表（编辑器里重新录入），USD 恒为基准 1 且置顶。 */
function parseRateRows(raw: string): RateRow[] {
  let obj: Record<string, number> = {}
  try {
    obj = JSON.parse(raw) as Record<string, number>
  } catch {
    return []
  }
  return Object.entries(obj)
    .sort((a, b) => (a[0] === 'USD' ? -1 : b[0] === 'USD' ? 1 : a[0].localeCompare(b[0])))
    .map(([code, rate]) => ({ code, rate: String(rate) }))
}

function serializeRateRows(rows: RateRow[]): string {
  const out: Record<string, number> = {}
  for (const r of rows) {
    const code = r.code.trim().toUpperCase()
    const rate = Number(r.rate)
    if (!/^[A-Z]{3}$/.test(code) || !Number.isFinite(rate) || rate <= 0) continue
    out[code] = code === 'USD' ? 1 : rate
  }
  return Object.keys(out).length ? JSON.stringify(out) : ''
}

const RATES_MAX = 24

/** 自定义 <head> 代码校验：后端原样以 template.HTML 注入页面 head，写坏一个未闭合的
    <script>/<style> 整站都会挂。只拦会破坏文档结构的写法——标签配对与文档级标签，
    不做完整 HTML 解析（管理员可信输入，统计脚本片段的合法形态有限）。 */
function headCodeInvalid(raw: string): 'script' | 'style' | 'docTag' | null {
  const v = raw.toLowerCase()
  if ((v.match(/<script\b/g) ?? []).length !== (v.match(/<\/script>/g) ?? []).length) return 'script'
  if ((v.match(/<style\b/g) ?? []).length !== (v.match(/<\/style>/g) ?? []).length) return 'style'
  if (/<\/?(head|body|html)\b/i.test(raw)) return 'docTag'
  return null
}

/** 员工规模 / 年营收 / 平均响应 / 主要市场的惯用档位（对标阿里店铺实力档案）。
    存量自由文本不在档位里时由 ConstantPicker 自动补进选项，不会丢显示。 */
const STAFF_COUNT_OPTIONS = ['1-50', '51-100', '101-500', '501-1000', '1000+']
const ANNUAL_REVENUE_OPTIONS = ['US$1M-10M', 'US$10M-50M', 'US$50M-100M', 'US$100M+']
const RESPONSE_TIME_OPTIONS = ['≤1h', '≤4h', '≤8h', '≤24h', '>24h']
const MAIN_MARKET_OPTIONS = ['欧洲', '北美', '南美', '东南亚', '中东', '非洲', '大洋洲', '东亚', '中亚', '全球']

/** 汇率表编辑器：替代手写 JSON——USD 是基准（恒 1、不可删改），其余币种行可增删改。
    序列化口径与后端 parseCurrencyRates 一致（^[A-Z]{3}$、rate>0、USD=1、最多 24 个）；
    行内非法的币种码/汇率不进序列化结果并就地标红，保存时后端仍会兜底校验。
    行状态是编辑期间的事实源：value 只在「外部变更」（保存后重取/主题导入）且不等于
    本地最近一次序列化结果时才重同步——每敲一键都拿 value 重挂载会把未填完的行清掉。 */
function CurrencyRatesEditor({ value, onChange, disabled }: { value: string; onChange: (v: string) => void; disabled?: boolean }) {
  const { t } = useTranslation()
  const fetchRates = useFetchCurrencyRates()
  const [rows, setRows] = useState<RateRow[]>(() => parseRateRows(value))
  const lastSerialized = useRef(serializeRateRows(parseRateRows(value)))
  useEffect(() => {
    if (value !== lastSerialized.current) {
      const next = parseRateRows(value)
      lastSerialized.current = serializeRateRows(next)
      setRows(next)
    }
  }, [value])
  const commit = (next: RateRow[]) => {
    setRows(next)
    const s = serializeRateRows(next)
    lastSerialized.current = s
    onChange(s)
  }
  const edit = (i: number, patch: Partial<RateRow>) => {
    commit(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  }
  // 一键拉取：只回填编辑器（走 commit 保证 value 同步），不写库——管理员确认后随设置一起保存。
  const handleFetch = () => {
    fetchRates.mutate(value, {
      onSuccess: (res) => {
        commit(parseRateRows(res.rates))
        toast.success(t('plugin_website.rates.fetchSuccess', { count: res.updated, source: res.source }))
      },
      onError: () => toast.error(t('plugin_website.rates.fetchFailed')),
    })
  }
  const codeInvalid = (r: RateRow) => r.code.trim() !== '' && !/^[A-Z]{3}$/.test(r.code.trim().toUpperCase())
  const rateInvalid = (r: RateRow) => r.code.trim() !== '' && r.code.trim().toUpperCase() !== 'USD' && (r.rate.trim() === '' || !(Number(r.rate) > 0))
  return (
    <div className="flex flex-col gap-2">
      {rows.length === 0 ? <p className="text-xs text-muted-foreground">{t('plugin_website.rates.empty')}</p> : null}
      <div className="flex flex-col gap-1.5">
        {rows.map((r, i) => {
          const isBase = r.code.trim().toUpperCase() === 'USD'
          return (
            <div key={i} className="flex items-center gap-2">
              <Input
                value={r.code}
                maxLength={3}
                disabled={disabled || isBase}
                aria-label={t('plugin_website.rates.codeLabel')}
                className={cn('w-24 font-mono uppercase', codeInvalid(r) && 'border-destructive')}
                onChange={(e) => edit(i, { code: e.target.value.toUpperCase() })}
                placeholder={t('plugin_website.rates.codePlaceholder')}
              />
              <Input
                value={isBase ? '1' : r.rate}
                disabled={disabled || isBase}
                inputMode="decimal"
                aria-label={t('plugin_website.rates.rateLabel')}
                className={cn('w-32', rateInvalid(r) && 'border-destructive')}
                onChange={(e) => edit(i, { rate: e.target.value })}
                placeholder={t('plugin_website.rates.ratePlaceholder')}
              />
              {isBase ? (
                <span className="shrink-0 text-xs text-muted-foreground">{t('plugin_website.rates.baseNote')}</span>
              ) : (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="size-8 shrink-0"
                  disabled={disabled}
                  aria-label={t('common.delete')}
                  onClick={() => commit(rows.filter((_, j) => j !== i))}
                >
                  <Trash2 aria-hidden="true" />
                </Button>
              )}
            </div>
          )
        })}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-fit"
          disabled={disabled || fetchRates.isPending || rows.length === 0}
          onClick={handleFetch}
        >
          <RefreshCw aria-hidden="true" className={cn(fetchRates.isPending && 'animate-spin')} />
          {fetchRates.isPending ? t('plugin_website.rates.fetching') : t('plugin_website.rates.fetch')}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-fit"
          disabled={disabled || rows.length >= RATES_MAX}
          onClick={() => commit([...rows, { code: '', rate: '' }])}
        >
          <Plus aria-hidden="true" />
          {t('plugin_website.rates.add')}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">{t('plugin_website.rates.hint', { max: RATES_MAX })}</p>
    </div>
  )
}

/**
 * 分区导航：按「谁在什么时候改它」切组，形态与平台「系统设置」一致
 * （左栏分区 + ?tab= 深链，只渲染当前分区）。整份表单仍是一次保存。
 */
const TABS = ['brand', 'home', 'seo', 'footer', 'features'] as const
type TabKey = (typeof TABS)[number]

/** 办公地点的可排序行：手柄拖拽 + 键盘（空格拿起、方向键移动）；条目之间只用分隔线，
    不再给每条套卡片框——「外层框 + 分组框 + 条目框」三层嵌套太重。 */
function SortableOfficeRow({
  index, disabled, onRemove, children,
}: {
  index: number
  disabled?: boolean
  onRemove: () => void
  children: ReactNode
}) {
  const { t } = useTranslation()
  const sortable = !disabled
  const { listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: String(index), disabled: !sortable })
  // 被拖行自身不做补间，跟手
  const style = { transform: CSS.Transform.toString(transform), transition: isDragging ? undefined : transition }
  return (
    <div
      ref={setNodeRef}
      style={style}
      className={cn(
        'flex gap-2 border-b border-border/60 py-3 first:pt-0 last:border-b-0 last:pb-0',
        isDragging && 'relative z-20 rounded-md bg-card/95 shadow-md',
      )}
    >
      <button
        type="button"
        aria-label={t('plugin_website.settings.officeDrag')}
        disabled={!sortable}
        className={cn(
          'mt-1 flex h-7 w-6 shrink-0 cursor-grab touch-none items-start justify-center rounded text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing',
          !sortable && 'pointer-events-none opacity-0',
        )}
        {...listeners}
      >
        <GripVertical className="size-4" aria-hidden="true" />
      </button>
      <div className="flex min-w-0 flex-1 flex-col gap-2">{children}</div>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-9 w-9 shrink-0"
        disabled={disabled}
        aria-label={t('common.delete')}
        onClick={onRemove}
      >
        <Trash2 aria-hidden="true" />
      </Button>
    </div>
  )
}

/**
 * 表单主体：整份表单只有一套，切语言换的是数据源。
 * 可翻译字段交给 LocaleFormProvider，站点名/logo/联系方式等共用字段留在本地 state。
 */
function SettingsFormBody({
  shared, onSharedChange, limits,
}: {
  shared: SharedSettings
  onSharedChange: (next: SharedSettings) => void
  limits: WebsiteLimits
}) {
  const { t } = useTranslation()
  const save = useSaveSettings()
  const lf = useLocaleForm()
  const { disabled: sharedDisabled } = useSharedField()
  // 分区深链：?tab= 与平台「系统设置」同一手法，切分区不重载页面。
  const [params, setParams] = useSearchParams()
  const wanted = params.get('tab')
  const tab: TabKey = TABS.find((k) => k === wanted) ?? 'brand'
  const go = (k: TabKey) => setParams({ tab: k }, { replace: true })
  // 语言清单：官网默认语言下拉的候选（与翻译表单同一来源，不再多拉一份）。
  const { data: languages = [] } = useLanguages()
  // 「设为首页」候选：全量页面（含草稿，草稿选了也不报错，发布后首页即生效）。
  const { data: pagesData } = usePages({ page: 1, pageSize: 200 })
  // 主题清单与演示内容导入：主题插件注册进来的模板包，保存设置后公开站生效。
  const { data: themesData } = useThemes()
  const seed = useSeedThemeDemo()
  // 分享图与封面同一套规则：https:// 或 / 开头，别的拦住——og:image 跟着指向它
  const ogInvalid = shared.ogImageUrl.trim() !== '' && !/^(https?:\/\/|\/)/i.test(shared.ogImageUrl.trim())
  // 成立年份：非空时必须在 1900 ~ 明年之间——手输年份挡手滑（0 = 留空不展示）。
  // 输满 4 位才判范围：半截输入（「20」→ 2015 的路上）不该闪错误。
  const estYearInvalid =
    shared.establishedYear > 0 &&
    String(shared.establishedYear).length >= 4 &&
    (shared.establishedYear < 1900 || shared.establishedYear > new Date().getFullYear() + 1)

  // 提示文案与「添加」按钮的上限都按后端下发的生效值渲染，同一组数值不再前后各抄一份。
  const mediaHint = { types: limits.media.labels.join(' / '), max: `${limits.media.maxMb}MB` }
  const socialMaxed = shared.socials.length >= limits.socialMax
  const friendMaxed = shared.friendLinks.length >= limits.friendMax

  const set = (key: SharedTextKey, v: string) => onSharedChange({ ...shared, [key]: v })

  // 当前选中主题（'' = default）：决定是否显示「导入演示内容」按钮。
  const selectedTheme = themesData?.items.find((x) => x.id === (shared.theme || 'default'))
  // 主题能力：填了也不会渲染的设置项要给提示，别让人以为保存坏了。
  // 传 shared.theme 而不是已保存值：表单里切主题后提示立刻跟着变。
  const { capOk, themeLabel } = useThemeCap(shared.theme)
  const capNote = (what: string) => t('plugin_website.settings.capUnsupported', { theme: themeLabel, what })
  const confirm = useConfirm()
  const importDemo = async () => {
    if (!selectedTheme) return
    if (
      !(await confirm({
        title: t('plugin_website.settings.demoImport'),
        description: t('plugin_website.settings.demoImportWarn'),
      }))
    ) {
      return
    }
    try {
      const rep = await seed.mutateAsync(selectedTheme.id)
      const total =
        rep.settings + rep.navs + rep.features + rep.categories + rep.pages + rep.posts + rep.productCategories + rep.products
      toast.success(t('plugin_website.settings.demoImported', { total, skipped: rep.skipped }))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const patchSocial = (i: number, patch: Partial<SocialLink>) =>
    onSharedChange({ ...shared, socials: shared.socials.map((s, idx) => (idx === i ? { ...s, ...patch } : s)) })
  const addSocial = () => onSharedChange({ ...shared, socials: [...shared.socials, { platform: 'github', url: '' }] })
  const removeSocial = (i: number) =>
    onSharedChange({ ...shared, socials: shared.socials.filter((_, idx) => idx !== i) })

  const patchFriend = (i: number, patch: Partial<FriendLink>) =>
    onSharedChange({ ...shared, friendLinks: shared.friendLinks.map((l, idx) => (idx === i ? { ...l, ...patch } : l)) })
  const addFriend = () => onSharedChange({ ...shared, friendLinks: [...shared.friendLinks, { name: '', url: '' }] })
  const removeFriend = (i: number) =>
    onSharedChange({ ...shared, friendLinks: shared.friendLinks.filter((_, idx) => idx !== i) })

  const patchOffice = (i: number, patch: Partial<WebsiteOffice>) =>
    onSharedChange({ ...shared, offices: shared.offices.map((o, idx) => (idx === i ? { ...o, ...patch } : o)) })

  // 地点拖拽排序：官网联系页地点卡按数组顺序渲染，这里的顺序就是展示顺序。
  const officeSensors = useSortableSensors()
  const onOfficeDragEnd = (e: DragEndEvent) => {
    const { active, over } = e
    if (!over || active.id === over.id) return
    const from = Number(active.id)
    const to = Number(over.id)
    if (Number.isNaN(from) || Number.isNaN(to)) return
    onSharedChange({ ...shared, offices: arrayMove(shared.offices, from, to) })
  }

  // 邮箱格式校验：设置侧此前没有（投稿侧有）；留空合法——联系邮箱选填
  const emailInvalid = shared.contactEmail.trim() !== '' && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(shared.contactEmail.trim())
  const messageRecipientInvalid = shared.messageRecipientEmail.trim() !== '' && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(shared.messageRecipientEmail.trim())
  // 留言同意开着时政策地址必填，格式与后端 footerURLValid 同规则：站内路径/锚点或完整 http(s) 地址。
  const consentInvalid = shared.contactConsent && !/^(https?:\/\/[^\s/]+|[#/])/i.test(shared.contactConsentUrl.trim())
  // 自定义 <head> 代码：拦截未闭合的 script/style 与文档级标签（校验规则见 headCodeInvalid）
  const headInvalid = headCodeInvalid(shared.headCode)
  // 未保存提示：shared 相对「载入/上次保存」的快照有变化即脏；刷新与关窗由浏览器原生拦截。
  // 多语言字段的改动在 Provider 内部（lf.dirty），必须一并计入——否则只改了「首页标题」
  // 这类可翻译字段时页面不脏，关标签页连提示都没有，改动直接丢。
  const savedRef = useRef<SharedSettings | null>(null)
  const [sharedDirty, setSharedDirty] = useState(false)
  useEffect(() => {
    if (shared && !savedRef.current) savedRef.current = shared
  }, [shared])
  useEffect(() => {
    if (shared && savedRef.current) setSharedDirty(JSON.stringify(shared) !== JSON.stringify(savedRef.current))
  }, [shared])
  const dirty = sharedDirty || lf.dirty
  useEffect(() => {
    if (!dirty) return
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  // 分区之间切换时回到顶部：路由没变，AppLayout 不会重置滚动位置，长分区切短分区会停在空白处。
  useEffect(() => {
    document.getElementById('main-content')?.scrollTo({ top: 0 })
  }, [tab])

  const submit = async () => {
    const base = lf.baseValues()
    if (emailInvalid) {
      toast.error(t('plugin_website.settings.emailInvalid'))
      return
    }
    if (messageRecipientInvalid) {
      toast.error(t('plugin_website.settings.messageRecipientInvalid'))
      return
    }
    if (consentInvalid) {
      toast.error(t('plugin_website.settings.consentUrlInvalid'))
      return
    }
    if (headInvalid) {
      if (headInvalid === 'script') toast.error(t('plugin_website.settings.headInvalidScript'))
      else if (headInvalid === 'style') toast.error(t('plugin_website.settings.headInvalidStyle'))
      else toast.error(t('plugin_website.settings.headInvalidDocTag'))
      return
    }
    if (estYearInvalid) {
      toast.error(t('plugin_website.settings.establishedYearInvalid'))
      return
    }
    try {
      await save.mutateAsync({
        ...shared,
        // 行上的裸列永远存原文；setting 不可按语言发布，只回传 i18nJson
        siteDesc: base.siteDesc ?? '',
        homeTitle: base.homeTitle ?? '',
        homeSubtitle: base.homeSubtitle ?? '',
        homeCtaLabel: base.homeCtaLabel ?? '',
        seoKeywords: base.seoKeywords ?? '',
        footerNote: base.footerNote ?? '',
        i18nJson: lf.serialize().i18nJson,
      })
      savedRef.current = shared
      setSharedDirty(false)
      // Provider 内部的脏标记也要清：不然保存完还会一直弹「未保存」提示
      lf.markSaved()
      toast.success(t('common.operateSuccess'))
    } catch {
      // 失败原因由 http 拦截器统一弹出，这里只做本地状态收尾
    }
  }

  const sharedInput = (key: SharedTextKey, error?: string, hint?: string) => (
    <SharedFieldShell key={key} labelKey={`plugin_website.fields.${key}`} hint={hint}>
      <Input
        value={shared[key]}
        disabled={sharedDisabled}
        aria-invalid={!!error}
        onChange={(e) => set(key, e.target.value)}
        placeholder={t(`plugin_website.placeholder.${key}`, { defaultValue: '' })}
      />
      {error ? <span className="text-xs text-destructive">{error}</span> : null}
    </SharedFieldShell>
  )

  const localizedField = (key: LocalizedKey, hint?: string) => (
    <LocalizedField
      key={key}
      fieldKey={key}
      hint={hint}
      placeholder={t(`plugin_website.placeholder.${key}`, { defaultValue: '' })}
    />
  )

  return (
    <div className="grid gap-6 md:grid-cols-[168px_minmax(0,1fr)]">
      {/* 窄屏收成下拉、宽屏用左栏分区导航：字段再多也只是导航变长，内容不再一列铺三四屏 */}
      <div className="md:hidden">
        <Select value={tab} onValueChange={(v) => go(v as TabKey)}>
          <SelectTrigger aria-label={t('plugin_website.settings.sectionNav')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {TABS.map((k) => (
              <SelectItem key={k} value={k}>{t(`plugin_website.settings.tab.${k}`)}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <nav aria-label={t('plugin_website.settings.sectionNav')} className="hidden flex-col gap-1 md:flex">
        {TABS.map((k) => (
          <button
            key={k}
            type="button"
            aria-current={tab === k ? 'page' : undefined}
            onClick={() => go(k)}
            className={cn(
              'rounded-md px-2 py-1.5 text-start text-sm transition-colors',
              // 选中态与平台系统设置左导航同一套语言：淡底 + 品牌色字，不填满
              tab === k
                ? 'bg-primary/10 font-medium text-primary'
                : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
            )}
          >
            {t(`plugin_website.settings.tab.${k}`)}
          </button>
        ))}
      </nav>
      <div className="flex min-w-0 flex-col gap-4">
        <div className="flex flex-wrap items-center justify-between gap-2 px-1">
          <LocaleSwitcher />
          <span className="text-xs text-muted-foreground">{t('plugin_website.i18n.switchHint')}</span>
        </div>
        {/* 分区标题就是左栏选中项，卡片里不再重复一遍眉题 */}
        <section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
          {tab === 'brand' ? (
            <>
              <SharedFieldShell labelKey="plugin_website.fields.theme" hint={t('plugin_website.hint.theme')}>
                {/* 默认主题在存储里的合法值是空串（后端 themeRegistered("") = true、
                    themeActive 以「非空且已注册」判品牌主题激活）。/themes 清单把默认主题
                    作为 id:"default" 的选项下发，选中后必须折回空串——原样存 "default" 会被
                    后端 website.theme_not_found 拒掉，且语义上等于激活了一个不存在的品牌主题 */}
                <Select
                  value={shared.theme || 'default'}
                  disabled={sharedDisabled}
                  onValueChange={(v) => set('theme', v === 'default' ? '' : v)}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(themesData?.items ?? [{ id: 'default', label: t('plugin_website.settings.defaultTheme'), version: '', hasDemo: false, active: true }]).map(
                      (th) => (
                        <SelectItem key={th.id} value={th.id}>
                          {th.label}
                          {th.version ? ` · ${th.version}` : ''}
                        </SelectItem>
                      ),
                    )}
                  </SelectContent>
                </Select>
              </SharedFieldShell>
              {selectedTheme?.hasDemo ? (
                <div className="flex items-center gap-2">
                  <Button type="button" variant="outline" size="sm" className="w-fit" disabled={seed.isPending} onClick={() => void importDemo()}>
                    {t('plugin_website.settings.demoImport')}
                  </Button>
                  <FieldHint text={t('plugin_website.hint.demoImport')} />
                </div>
              ) : null}
              {sharedInput('siteName')}
              {localizedField('siteDesc')}
              {/* 图片字段统一走 ImageUrlField（上传 + 地址输入 + 从文件库选择，与产品管理同款入口）；
                  裁剪规格照旧：分享图 1200×630，其余自由裁剪 */}
              <SharedFieldShell labelKey="plugin_website.fields.logoUrl" hint={t('plugin_website.hint.logoUrl', mediaHint)}>
                <ImageUrlField
                  value={shared.logoUrl}
                  onChange={(v) => set('logoUrl', v)}
                  title={t('plugin_website.fields.logoUrl')}
                  crop="free"
                  disabled={sharedDisabled}
                  placeholder={t('plugin_website.placeholder.logoUrl', { defaultValue: '' })}
                  previewBelow
                  aria-label={t('plugin_website.fields.logoUrl')}
                />
              </SharedFieldShell>
              <SharedFieldShell labelKey="plugin_website.fields.faviconUrl" hint={t('plugin_website.hint.faviconUrl', mediaHint)}>
                <ImageUrlField
                  value={shared.faviconUrl}
                  onChange={(v) => set('faviconUrl', v)}
                  title={t('plugin_website.fields.faviconUrl')}
                  crop="free"
                  disabled={sharedDisabled}
                  placeholder={t('plugin_website.placeholder.faviconUrl', { defaultValue: '' })}
                  previewBelow
                  aria-label={t('plugin_website.fields.faviconUrl')}
                />
              </SharedFieldShell>
            </>
          ) : null}
          {tab === 'home' ? (
            <>
              {localizedField('homeTitle', t('plugin_website.hint.homeTitle'))}
              {localizedField('homeSubtitle')}
              {localizedField('homeCtaLabel')}
              {sharedInput('homeCtaHref')}
              {/* 首页主视觉覆盖：四套主题都消费 heroImageUrl；默认主题深浅双图可分别指定。 */}
              <SharedFieldShell labelKey="plugin_website.fields.heroImageUrl" hint={t('plugin_website.hint.heroImageUrl', mediaHint)}>
                <ImageUrlField
                  value={shared.heroImageUrl}
                  onChange={(v) => set('heroImageUrl', v)}
                  title={t('plugin_website.fields.heroImageUrl')}
                  crop="free"
                  disabled={sharedDisabled}
                  placeholder={t('plugin_website.placeholder.heroImageUrl', { defaultValue: '' })}
                  previewBelow
                  aria-label={t('plugin_website.fields.heroImageUrl')}
                />
              </SharedFieldShell>
              <SharedFieldShell labelKey="plugin_website.fields.heroImageDarkUrl" hint={t('plugin_website.hint.heroImageDarkUrl', mediaHint)}>
                <ImageUrlField
                  value={shared.heroImageDarkUrl}
                  onChange={(v) => set('heroImageDarkUrl', v)}
                  title={t('plugin_website.fields.heroImageDarkUrl')}
                  crop="free"
                  disabled={sharedDisabled}
                  placeholder={t('plugin_website.placeholder.heroImageDarkUrl', { defaultValue: '' })}
                  previewBelow
                  aria-label={t('plugin_website.fields.heroImageDarkUrl')}
                />
              </SharedFieldShell>
              {/* 设为首页：把某张自定义单页顶到 / 上；空 = 内置 Hero 页。Radix Select 不收空串值，用哨兵。 */}
              <SharedFieldShell labelKey="plugin_website.fields.homeSlug" hint={t('plugin_website.hint.homeSlug')}>
                <Select
                  value={shared.homeSlug || '__builtin'}
                  disabled={sharedDisabled}
                  onValueChange={(v) => set('homeSlug', v === '__builtin' ? '' : v)}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__builtin">{t('plugin_website.settings.homeBuiltin')}</SelectItem>
                    {(pagesData?.list ?? []).map((p) => (
                      <SelectItem key={p.id} value={p.slug}>
                        {p.title}
                        {!p.published ? `（${t('plugin_website.publishedOff')}）` : ''}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </SharedFieldShell>
            </>
          ) : null}
          {tab === 'seo' ? (
            <>
              {sharedInput('siteOrigin', undefined, t('plugin_website.hint.siteOrigin'))}
              {localizedField('seoKeywords')}
              {sharedInput('titleSuffix', undefined, t('plugin_website.hint.titleSuffix'))}
              <SharedFieldShell labelKey="plugin_website.fields.ogImageUrl" hint={t('plugin_website.hint.ogImageUrl')}>
                <ImageUrlField
                  value={shared.ogImageUrl}
                  onChange={(v) => set('ogImageUrl', v)}
                  title={t('plugin_website.fields.ogImageUrl')}
                  crop={{ width: 1200, height: 630 }}
                  disabled={sharedDisabled}
                  placeholder={t('plugin_website.placeholder.ogImageUrl', { defaultValue: '' })}
                  previewBelow
                  aria-label={t('plugin_website.fields.ogImageUrl')}
                />
                {ogInvalid ? <span className="text-xs text-destructive">{t('plugin_website.fields.coverInvalid')}</span> : null}
              </SharedFieldShell>
              <SharedFieldShell labelKey="plugin_website.fields.headCode" hint={t('plugin_website.hint.headCode')}>
                <Textarea
                  rows={4}
                  value={shared.headCode}
                  disabled={sharedDisabled}
                  maxLength={5000}
                  aria-invalid={!!headInvalid}
                  onChange={(e) => set('headCode', e.target.value)}
                  placeholder={t('plugin_website.placeholder.headCode', { defaultValue: '' })}
                />
                {headInvalid ? (
                  <span className="text-xs text-destructive">
                    {headInvalid === 'script'
                      ? t('plugin_website.settings.headInvalidScript')
                      : headInvalid === 'style'
                        ? t('plugin_website.settings.headInvalidStyle')
                        : t('plugin_website.settings.headInvalidDocTag')}
                  </span>
                ) : null}
              </SharedFieldShell>
              <SharedFieldShell labelKey="plugin_website.fields.robotsTxt" hint={t('plugin_website.hint.robotsTxt')}>
                <Textarea
                  rows={5}
                  value={shared.robotsTxt}
                  disabled={sharedDisabled}
                  maxLength={5000}
                  onChange={(e) => set('robotsTxt', e.target.value)}
                  placeholder={t('plugin_website.placeholder.robotsTxt', { defaultValue: '' })}
                />
              </SharedFieldShell>
              {/* 官网默认语言：跟随浏览器（auto）或指定语言占根路径；原文语言（平台默认）只作标记不隐藏。
                  Radix Select 不收空串值，用 auto 哨兵，与 homeSlug 同一手法。 */}
              <SharedFieldShell labelKey="plugin_website.fields.defaultLang" hint={t('plugin_website.hint.defaultLang')}>
                <Select
                  value={shared.defaultLang || 'auto'}
                  disabled={sharedDisabled}
                  onValueChange={(v) => set('defaultLang', v === 'auto' ? '' : v)}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="auto">{t('plugin_website.settings.defaultLangAuto')}</SelectItem>
                    {languages.map((l) => (
                      <SelectItem key={l.code} value={l.code}>
                        {l.label}
                        {l.default ? `（${t('plugin_website.settings.defaultLangSource')}）` : ''}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </SharedFieldShell>
              {/* 语言清单已归平台管理（系统设置 → 语言管理），这里只给入口提示，不再复制一份编辑器 */}
              <p className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                {t('plugin_website.languages.manageShort')}
                <FieldHint text={t('plugin_website.languages.manageHint')} />
              </p>
              {/* 币种换算（对标阿里右上角「语言-币种」选择器）：汇率表曾是手写 JSON 文本，
                  极易写错（大小写/引号/非数字），改为结构化行编辑器——USD 恒为基准 1；
                  表非空时公开站头部出「语言-币种」弹层，价格按访客所选币种换算展示。
                  默认币种下拉的选项直接取汇率表的键。 */}
              {capOk('currency') ? null : <ThemeCapNote text={capNote(t('plugin_website.settings.capCurrency'))} />}
              <SharedFieldShell labelKey="plugin_website.fields.currencyRates" hint={t('plugin_website.hint.currencyRates')}>
                <CurrencyRatesEditor
                  value={shared.currencyRates}
                  disabled={sharedDisabled}
                  onChange={(v) => set('currencyRates', v)}
                />
              </SharedFieldShell>
              {shared.currencyRates.trim() ? (
                <SharedFieldShell labelKey="plugin_website.fields.defaultCurrency" hint={t('plugin_website.hint.defaultCurrency')}>
                  {/* Radix Select 不收空串值，用 off 哨兵表示「不启用换算」，与 defaultLang 的 auto 同一手法 */}
                  <Select
                    value={shared.defaultCurrency || 'off'}
                    disabled={sharedDisabled}
                    onValueChange={(v) => set('defaultCurrency', v === 'off' ? '' : v)}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="off">{t('plugin_website.settings.defaultCurrencyOff')}</SelectItem>
                      {currencyRatesKeys(shared.currencyRates).map((code) => (
                        <SelectItem key={code} value={code}>{code}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </SharedFieldShell>
              ) : null}
            </>
          ) : null}
          {tab === 'footer' ? (
            <>
              {sharedInput('contactEmail', emailInvalid ? t('plugin_website.settings.emailInvalid') : undefined)}
              {sharedInput('messageRecipientEmail', messageRecipientInvalid ? t('plugin_website.settings.messageRecipientInvalid') : undefined, t('plugin_website.hint.messageRecipientEmail'))}
              {sharedInput('contactPhone')}
              {/* 供应商实力档案：商详左栏供应商卡的五行数据，对齐阿里的「店铺实力」；
                  全空就不展示那一块，所以这里不设必填 */}
              <div className="flex flex-col gap-3 rounded-lg border border-border p-3">
                <div className="flex flex-col gap-0.5">
                  <span className="text-sm font-medium">{t('plugin_website.settings.supplierProfile')}</span>
                  <span className="text-xs text-muted-foreground">{t('plugin_website.settings.supplierProfileHint')}</span>
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  <SharedFieldShell labelKey="plugin_website.fields.establishedYear" hint={t('plugin_website.hint.establishedYear')}>
                    {/* 年份 = 精确数字，走输入框而不是 57 项下拉（同组字段口径：留空即隐藏，
                        「不展示」不再做成哨兵选项——那是给离散选项用的，年份是连续值） */}
                    <Input
                      value={shared.establishedYear > 0 ? String(shared.establishedYear) : ''}
                      inputMode="numeric"
                      disabled={sharedDisabled}
                      placeholder={t('plugin_website.placeholder.establishedYear', { defaultValue: '' })}
                      aria-label={t('plugin_website.fields.establishedYear')}
                      aria-invalid={estYearInvalid}
                      onChange={(e) => {
                        const digits = e.target.value.replace(/\D/g, '').slice(0, 4)
                        onSharedChange({ ...shared, establishedYear: digits === '' ? 0 : Number(digits) })
                      }}
                    />
                    {estYearInvalid ? <span className="text-xs text-destructive">{t('plugin_website.settings.establishedYearInvalid')}</span> : null}
                  </SharedFieldShell>
                  <SharedFieldShell labelKey="plugin_website.fields.staffCount">
                    <ConstantPicker
                      value={shared.staffCount}
                      onChange={(v) => set('staffCount', v)}
                      disabled={sharedDisabled}
                      allowCustom
                      placeholder={t('plugin_website.placeholder.staffCount', { defaultValue: '' })}
                      aria-label={t('plugin_website.fields.staffCount')}
                      options={STAFF_COUNT_OPTIONS.map((v) => ({ value: v, label: v }))}
                    />
                  </SharedFieldShell>
                  <SharedFieldShell labelKey="plugin_website.fields.annualRevenue">
                    <ConstantPicker
                      value={shared.annualRevenue}
                      onChange={(v) => set('annualRevenue', v)}
                      disabled={sharedDisabled}
                      allowCustom
                      placeholder={t('plugin_website.placeholder.annualRevenue', { defaultValue: '' })}
                      aria-label={t('plugin_website.fields.annualRevenue')}
                      options={ANNUAL_REVENUE_OPTIONS.map((v) => ({ value: v, label: v }))}
                    />
                  </SharedFieldShell>
                  <SharedFieldShell labelKey="plugin_website.fields.mainMarkets">
                    <ConstantPicker
                      value={shared.mainMarkets}
                      onChange={(v) => set('mainMarkets', v)}
                      disabled={sharedDisabled}
                      allowCustom
                      multiple
                      placeholder={t('plugin_website.placeholder.mainMarkets', { defaultValue: '' })}
                      aria-label={t('plugin_website.fields.mainMarkets')}
                      options={MAIN_MARKET_OPTIONS.map((v) => ({ value: v, label: v }))}
                    />
                  </SharedFieldShell>
                  <SharedFieldShell labelKey="plugin_website.fields.responseTime">
                    <ConstantPicker
                      value={shared.responseTime}
                      onChange={(v) => set('responseTime', v)}
                      disabled={sharedDisabled}
                      allowCustom
                      placeholder={t('plugin_website.placeholder.responseTime', { defaultValue: '' })}
                      aria-label={t('plugin_website.fields.responseTime')}
                      options={RESPONSE_TIME_OPTIONS.map((v) => ({ value: v, label: v }))}
                    />
                  </SharedFieldShell>
                </div>
                {/* 办公地点：联系页地点卡 + 地图打点（厂商按访客语言判定，Key 在「系统设置 → 地图服务」）。
                    坐标用地图插件的位置字段选点，没装插件也能手填经纬度。
                    条目不再各自套卡片框（三层框太重）：外层一个框，条目间用分隔线；
                    顺序支持拖拽（官网联系页地点卡按数组顺序渲染）。 */}
                <div className="flex flex-col gap-1 rounded-lg border border-border p-3">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex flex-col gap-0.5">
                      <span className="text-sm font-medium">{t('plugin_website.settings.offices')}</span>
                      <span className="text-xs text-muted-foreground">{t('plugin_website.settings.officesHint')}</span>
                      {capOk('offices') ? null : <ThemeCapNote text={capNote(t('plugin_website.settings.offices'))} />}
                    </div>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={sharedDisabled || shared.offices.length >= 10}
                      onClick={() => onSharedChange({ ...shared, offices: [...shared.offices, { name: '', address: '', lng: 0, lat: 0 }] })}
                    >
                      <Plus aria-hidden="true" />
                      {t('plugin_website.settings.officeAdd')}
                    </Button>
                  </div>
                  {shared.offices.length > 0 ? (
                    <DndContext
                      sensors={officeSensors}
                      collisionDetection={pointerFirstCollision}
                      onDragOver={(e) => {
                        const w = (window as unknown as Record<string, unknown>)
                        w.__dndOver = String(e.over?.id ?? 'null')
                        w.__dndDelta = String(e.delta.y)
                      }}
                      onDragEnd={(e) => { (window as unknown as Record<string, unknown>).__dndOver = 'end:' + String(e.over?.id ?? 'null'); onOfficeDragEnd(e) }}
                      onDragCancel={() => { (window as unknown as Record<string, unknown>).__dndOver = 'canceled' }}
                    >
                      <DragCursorHost />
                      <SortableContext items={shared.offices.map((_, i) => String(i))} strategy={verticalListSortingStrategy}>
                        <div className="flex flex-col">
                          {shared.offices.map((o, i) => (
                            <SortableOfficeRow key={i} index={i} disabled={sharedDisabled} onRemove={() => onSharedChange({ ...shared, offices: shared.offices.filter((_, j) => j !== i) })}>
                              <Input
                                value={o.name}
                                disabled={sharedDisabled}
                                maxLength={100}
                                placeholder={t('plugin_website.placeholder.officeName', { defaultValue: '' })}
                                aria-label={t('plugin_website.fields.officeName')}
                                onChange={(e) => patchOffice(i, { name: e.target.value })}
                              />
                              <LocationField
                                value={{ lng: o.lng, lat: o.lat, address: o.address }}
                                onChange={(p) => patchOffice(i, { lng: p.lng, lat: p.lat, address: p.address ?? '' })}
                                disabled={sharedDisabled}
                              />
                            </SortableOfficeRow>
                          ))}
                        </div>
                      </SortableContext>
                    </DndContext>
                  ) : (
                    <p className="py-2 text-xs text-muted-foreground">{t('plugin_website.settings.officesEmpty')}</p>
                  )}
                </div>
              </div>
              <SharedFieldShell labelKey="plugin_website.fields.qrCodeUrl" hint={t('plugin_website.hint.qrCodeUrl', mediaHint)}>
                <ImageUrlField
                  value={shared.qrCodeUrl}
                  onChange={(v) => set('qrCodeUrl', v)}
                  title={t('plugin_website.fields.qrCodeUrl')}
                  crop="free"
                  disabled={sharedDisabled}
                  placeholder={t('plugin_website.placeholder.qrCodeUrl', { defaultValue: '' })}
                  previewClassName="bg-background"
                  previewBelow
                  aria-label={t('plugin_website.fields.qrCodeUrl')}
                />
              </SharedFieldShell>
              {sharedInput('qrCodeCaption')}
              {sharedInput('icp')}
              {localizedField('footerNote')}
              <div className="flex flex-col gap-2">
                <span className="inline-flex items-center gap-1.5 text-sm font-medium">
                  {t('plugin_website.settings.socials')}
                  <FieldHint text={t('plugin_website.hint.socials', { max: limits.socialMax })} />
                </span>
                {shared.socials.map((s, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <Select
                      value={s.platform}
                      disabled={sharedDisabled}
                      onValueChange={(v) => patchSocial(i, { platform: v as SocialPlatform })}
                    >
                      <SelectTrigger className="w-32 shrink-0" aria-label={t('plugin_website.settings.socialPlatform')}>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {SOCIAL_PLATFORMS.map((p) => (
                          <SelectItem key={p} value={p}>{t(`plugin_website.social.${p}`)}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Input
                      value={s.url}
                      disabled={sharedDisabled}
                      onChange={(e) => patchSocial(i, { url: e.target.value })}
                      placeholder={t('plugin_website.placeholder.socialUrl', { defaultValue: '' })}
                      aria-label={t(`plugin_website.social.${s.platform}`)}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      disabled={sharedDisabled}
                      aria-label={t('common.delete')}
                      onClick={() => removeSocial(i)}
                    >
                      <Trash2 className="size-4" aria-hidden="true" />
                    </Button>
                  </div>
                ))}
                <Button type="button" variant="outline" size="sm" className="w-fit" disabled={sharedDisabled || socialMaxed} onClick={addSocial}>
                  <Plus className="size-4" aria-hidden="true" />
                  {t('plugin_website.settings.addSocial')}
                </Button>
              </div>
              <div className="flex flex-col gap-2">
                <span className="inline-flex items-center gap-1.5 text-sm font-medium">
                  {t('plugin_website.settings.friendLinks')}
                  <FieldHint text={t('plugin_website.hint.friendLinks', { max: limits.friendMax })} />
                </span>
                {capOk('friendLinks') ? null : <ThemeCapNote text={capNote(t('plugin_website.settings.friendLinks'))} />}
                {shared.friendLinks.map((l, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <Input
                      className="w-40 shrink-0"
                      value={l.name}
                      disabled={sharedDisabled}
                      onChange={(e) => patchFriend(i, { name: e.target.value })}
                      placeholder={t('plugin_website.placeholder.friendName', { defaultValue: '' })}
                      aria-label={t('plugin_website.settings.friendName')}
                    />
                    <Input
                      value={l.url}
                      disabled={sharedDisabled}
                      onChange={(e) => patchFriend(i, { url: e.target.value })}
                      placeholder={t('plugin_website.placeholder.friendUrl', { defaultValue: '' })}
                      aria-label={t('plugin_website.settings.friendUrl')}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      disabled={sharedDisabled}
                      aria-label={t('common.delete')}
                      onClick={() => removeFriend(i)}
                    >
                      <Trash2 className="size-4" aria-hidden="true" />
                    </Button>
                  </div>
                ))}
                <Button type="button" variant="outline" size="sm" className="w-fit" disabled={sharedDisabled || friendMaxed} onClick={addFriend}>
                  <Plus className="size-4" aria-hidden="true" />
                  {t('plugin_website.settings.addFriendLink')}
                </Button>
              </div>
            </>
          ) : null}
          {tab === 'features' ? (
            // 开关收进一个带分隔线的组（bare 行，与产品/文章发布卡片同一套语言），
            // 不再是卡片里一条开关套一个边框的「盒中盒」。
            <div className="divide-y divide-border rounded-lg border border-border">
              <SwitchRow
                bare
                className="px-3 py-2.5"
                label={t('plugin_website.chat.enabled')}
                description={t('plugin_website.chat.enabledHint')}
                checked={shared.chatEnabled}
                disabled={sharedDisabled}
                onCheckedChange={(v) => onSharedChange({ ...shared, chatEnabled: v })}
              />
              <SwitchRow
                bare
                className="px-3 py-2.5"
                label={t('plugin_website.message.notifyEmail')}
                description={t('plugin_website.message.notifyEmailHint')}
                checked={shared.messageNotifyEmail}
                disabled={sharedDisabled}
                onCheckedChange={(v) => onSharedChange({ ...shared, messageNotifyEmail: v })}
              />
              <div className="flex flex-col gap-3 px-3 py-2.5">
                <SwitchRow
                  bare
                  label={t('plugin_website.message.consent')}
                  description={t('plugin_website.message.consentHint')}
                  checked={shared.contactConsent}
                  disabled={sharedDisabled}
                  onCheckedChange={(v) => onSharedChange({ ...shared, contactConsent: v })}
                />
                {/* 政策地址只在开启后出现：关着时摆一个必填框只会让人以为现在就在拦投稿。
                    缩进挂在开关下面，不另起一格，避免分隔线把它和开关拆开。 */}
                {shared.contactConsent ? (
                  <div className="ps-5">
                    {sharedInput('contactConsentUrl', consentInvalid ? t('plugin_website.settings.consentUrlInvalid') : undefined)}
                  </div>
                ) : null}
              </div>
              <SwitchRow
                bare
                className="px-3 py-2.5"
                label={t('plugin_website.market.enabled')}
                description={
                  <>
                    {t('plugin_website.market.enabledHint')}
                    {capOk('market') ? null : <span className="mt-1 block text-warning">{capNote(t('plugin_website.market.enabled'))}</span>}
                  </>
                }
                checked={shared.marketEnabled}
                disabled={sharedDisabled}
                onCheckedChange={(v) => onSharedChange({ ...shared, marketEnabled: v })}
              />
              <SwitchRow
                bare
                className="px-3 py-2.5"
                label={t('plugin_website.purchase.enabled')}
                description={t('plugin_website.purchase.enabledHint')}
                checked={shared.purchaseEnabled}
                disabled={sharedDisabled}
                onCheckedChange={(v) => onSharedChange({ ...shared, purchaseEnabled: v })}
              />
            </div>
          ) : null}
        </section>
        <LocaleFormActions />
        {/* 保存条常驻：五个分区共用一份表单，切分区不丢改动，也不该让人以为每个分区各存一次。
            「有未保存的修改」文案撤掉：保存按钮就在旁边，这行字只会制造噪音（2026-11-20 用户反馈）。 */}
        <div className="flex items-center gap-3">
          <Button onClick={() => void submit()} disabled={save.isPending || emailInvalid || messageRecipientInvalid || consentInvalid || !!headInvalid || estYearInvalid}>{t('common.save')}</Button>
        </div>
      </div>
    </div>
  )
}

export default function SettingsPage() {
  const { t } = useTranslation()
  const { data, isLoading } = useSettings()
  const { data: languages = [] } = useLanguages()
  const { data: schema } = useI18nSchema('setting')
  const { data: limits } = useWebsiteLimits()
  const defaultLocale = languages.find((l) => l.default)?.code ?? ''
  const translate = useCallback<TranslateBatch>(
    (texts, to, format) => translateContent('setting', texts, defaultLocale, to, format),
    [defaultLocale],
  )
  const [shared, setShared] = useState<SharedSettings | null>(null)

  useEffect(() => {
    if (data && !shared) {
      setShared({
        siteName: data.siteName,
        logoUrl: data.logoUrl,
        faviconUrl: data.faviconUrl ?? '',
        ogImageUrl: data.ogImageUrl ?? '',
        headCode: data.headCode ?? '',
        titleSuffix: data.titleSuffix ?? '',
        heroImageUrl: data.heroImageUrl ?? '',
        heroImageDarkUrl: data.heroImageDarkUrl ?? '',
        robotsTxt: data.robotsTxt ?? '',
        homeSlug: data.homeSlug ?? '',
        siteOrigin: data.siteOrigin ?? '',
        homeCtaHref: data.homeCtaHref,
        theme: data.theme ?? '',
        defaultLang: data.defaultLang ?? '',
        defaultCurrency: data.defaultCurrency ?? '',
        currencyRates: data.currencyRates ?? '',
        contactEmail: data.contactEmail,
        contactPhone: data.contactPhone,
        qrCodeUrl: data.qrCodeUrl,
        qrCodeCaption: data.qrCodeCaption,
        icp: data.icp,
        marketEnabled: data.marketEnabled,
        purchaseEnabled: data.purchaseEnabled ?? false,
        messageNotifyEmail: data.messageNotifyEmail,
        messageRecipientEmail: data.messageRecipientEmail ?? '',
        chatEnabled: data.chatEnabled,
        contactConsent: data.contactConsent,
        contactConsentUrl: data.contactConsentUrl ?? '',
        establishedYear: data.establishedYear ?? 0,
        staffCount: data.staffCount ?? '',
        annualRevenue: data.annualRevenue ?? '',
        mainMarkets: data.mainMarkets ?? '',
        responseTime: data.responseTime ?? '',
        socials: data.socials ?? [],
        friendLinks: data.friendLinks ?? [],
        offices: data.offices ?? [],
      })
    }
  }, [data, shared])

  // schema、语言清单、设置与生效上限都是表单的前提，没到货前只出骨架屏——否则会闪出一个没有字段的空表单。
  if (isLoading || !data || !shared || !schema || !defaultLocale || !limits) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-64 w-full max-w-2xl" />
      </div>
    )
  }

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_website.settings.title')}
        description={t('plugin_website.settings.desc')}
        actions={
          // 改完就地跳去看：配了规范域名（siteOrigin）就用它，否则按同源根路径打开官网
          <Button asChild variant="outline" size="sm">
            <a href={shared.siteOrigin.trim() || '/'} target="_blank" rel="noreferrer">
              <ExternalLink aria-hidden="true" />
              {t('plugin_website.viewSite')}
            </a>
          </Button>
        }
      />
      <div className="flex w-full flex-col gap-4">
        <LocaleFormProvider
          schema={schema}
          locales={languages}
          defaultLocale={defaultLocale}
          base={{
            siteDesc: data.siteDesc,
            homeTitle: data.homeTitle,
            homeSubtitle: data.homeSubtitle,
            homeCtaLabel: data.homeCtaLabel,
            seoKeywords: data.seoKeywords,
            footerNote: data.footerNote,
          }}
          i18nJson={data.i18nJson}
          translate={translate}
        >
          <SettingsFormBody shared={shared} onSharedChange={setShared} limits={limits} />
        </LocaleFormProvider>
      </div>
    </PageContainer>
  )
}
