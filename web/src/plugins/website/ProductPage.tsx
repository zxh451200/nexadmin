import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { CircleAlert, CircleCheck, CloudUpload, Copy, Download, ExternalLink, Eye, FileUp, Pencil, Plus, Settings2, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Can } from '@/framework/auth/Can'
import { OptionPicker } from '@/framework/components/OptionPicker'
import { useAsyncJob } from '@/framework/hooks/use-async-job'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
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
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchRow } from '@/framework/components/SwitchField'
import { Lightbox } from '@/framework/upload/Lightbox'
import { FileDropField } from '@/framework/components/FileDropField'
import { ImageUrlField } from '@/framework/components/ImageUrlField'
import { Uploader } from '@/framework/components/Uploader'
import { VideoUrlField } from '@/framework/components/VideoUrlField'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { OptionList, type Option } from '@/framework/components/pickers/OptionList'
import { PickerShell } from '@/framework/components/pickers/PickerShell'
import { PickerTrigger } from '@/framework/components/pickers/PickerTrigger'
import { CURRENCIES, PAYMENT_TERMS, PORTS, UNITS } from './constants'
import { downloadCsv, saveBlob } from '@/framework/components/export/csv'
import { formatTime, slugify } from './format'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { EditorSchemaGate } from './EditorSchemaGate'
import TranslationStatus from './TranslationStatus'
import {
  fetchAliCategories,
  fetchAliTest,
  fetchProductExportPlan,
  fetchProductImportJob,
  fetchProductImportTemplate,
  fetchProductPreview,
  fetchProductsForExport,
  createProductExportJob,
  translateContent,
  useAliConfig,
  useAliSyncStatus,
  useBindAliCategory,
  useCopyProduct,
  useDeleteProduct,
  useDeleteProducts,
  useI18nSchema,
  useLanguages,
  useProductCategories,
  useProducts,
  useRunProductImport,
  useSaveAliConfig,
  useSaveProduct,
  useSetProductStatus,
  useSyncProduct,
  useSyncProductBatch,
  toContentQuery,
  type AliCategoryNode,
  type AliMarket,
  type ContentDraft,
  type ContentQuery,
  type ProductCategoryNode,
  type ProductImportJob,
  type ProductSpec,
  type ProductStatus,
  type SiteProduct,
} from './api'

const ALL = '__all'

/** 图集上限，与后端 maxImagesPerProduct 同值：界面先挡住，别等保存才报错。 */
const IMAGES_MAX = 10

/** 关键词个数上限，与后端 maxKeywordsPerProduct 同值：预览与提示都以它为准。 */
const maxKeywords = 3

function statusBadge(status: ProductStatus, t: (k: string) => string) {
  switch (status) {
    case 'online':
      return <Badge variant="success">{t('plugin_website.product.statusOnline')}</Badge>
    case 'offline':
      return <Badge variant="warning">{t('plugin_website.product.statusOffline')}</Badge>
    case 'pending':
      return <Badge variant="info">{t('plugin_website.product.statusPending')}</Badge>
    case 'rejected':
      return <Badge variant="destructive">{t('plugin_website.product.statusRejected')}</Badge>
    default:
      return <Badge variant="outline">{t('plugin_website.product.statusDraft')}</Badge>
  }
}

function syncBadge(r: SiteProduct, t: (k: string) => string) {
  switch (r.syncStatus) {
    case 'synced':
      return <Badge variant="success">{t('plugin_website.product.syncSynced')}</Badge>
    case 'pending':
      return <Badge variant="info">{t('plugin_website.product.syncPending')}</Badge>
    case 'failed':
      return <Badge variant="destructive">{t('plugin_website.product.syncFailed')}</Badge>
    default:
      return <span className="text-xs text-muted-foreground">{t('plugin_website.product.syncNone')}</span>
  }
}

/** 同步状态的纯文本形态（CSV 导出用）：键与 syncBadge 同源。 */
function syncText(r: SiteProduct, t: (k: string) => string): string {
  switch (r.syncStatus) {
    case 'synced':
      return t('plugin_website.product.syncSynced')
    case 'pending':
      return t('plugin_website.product.syncPending')
    case 'failed':
      return t('plugin_website.product.syncFailed')
    default:
      return t('plugin_website.product.syncNone')
  }
}

/** 价格区间的展示形态：未定价显示「询盘」，区间相同只显示一个数。 */
function priceText(r: SiteProduct): string {
  if (!r.priceMin) return ''
  const min = r.priceMin.toFixed(2)
  const max = r.priceMax > r.priceMin ? ` - ${r.priceMax.toFixed(2)}` : ''
  return `${min}${max} ${r.currency}${r.priceUnit ? ` / ${r.priceUnit}` : ''}`
}

/** 平铺类目按 parentId 组树并标注深度，供表单的复选框缩进展示。 */
interface CatNode extends ProductCategoryNode {
  depth: number
  children?: CatNode[]
}

function buildCatTree(rows: ProductCategoryNode[]): CatNode[] {
  const byId = new Map<string, CatNode>()
  for (const row of rows) byId.set(row.id, { ...row, depth: 1 })
  const roots: CatNode[] = []
  for (const node of byId.values()) {
    const parent = node.parentId && node.parentId !== '0' ? byId.get(node.parentId) : undefined
    if (parent) (parent.children ??= []).push(node)
    else roots.push(node)
  }
  const walk = (nodes: CatNode[], depth: number) => {
    for (const n of nodes) {
      n.depth = depth
      walk(n.children ?? [], depth + 1)
    }
  }
  walk(roots, 1)
  return roots
}

function flattenCatTree(nodes: CatNode[], out: CatNode[] = []): CatNode[] {
  for (const n of nodes) {
    out.push(n)
    flattenCatTree(n.children ?? [], out)
  }
  return out
}

/** 价格数字输入的取值：空串归 0，负数归 0（服务端同口径，输入端先归一避免闪红）。 */
function parseNum(v: string): number {
  const n = Number(v)
  return Number.isFinite(n) && n > 0 ? n : 0
}

/** 关键词拆分预览：与后端 normalizeProductKeywords 同口径——空格/中英文逗号/顿号/分号都算分隔符，忽略大小写去重。 */
function splitKeywordsPreview(s: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of s.split(/[ \t\n\r,，、;；]+/)) {
    const f = raw.trim()
    if (!f) continue
    const k = f.toLowerCase()
    if (seen.has(k)) continue
    seen.add(k)
    out.push(f)
  }
  return out
}

/** 关键词实时回显：忘了打空格时，「只识别出 1 个」当场就能看见，而不是等同步阿里才发现。 */
function KeywordHint({ value }: { value: string }) {
  const { t } = useTranslation()
  const words = useMemo(() => splitKeywordsPreview(value), [value])
  if (!value.trim() || !words.length) return null
  const over = words.length > maxKeywords
  return (
    <p className={cn('text-xs', over ? 'text-warning' : 'text-muted-foreground')}>
      {t(
        over ? 'plugin_website.fields.keywordsPreviewOver' : 'plugin_website.fields.keywordsPreview',
        { count: words.length, words: words.slice(0, maxKeywords).join(' / ') },
      )}
    </p>
  )
}

/** 币种选项：代码 + Intl 现算的本地化名称（USD · 美元），随界面语言变。 */
function useCurrencyOptions(): Option[] {
  const { i18n } = useTranslation()
  return useMemo(() => {
    let dn: Intl.DisplayNames | undefined
    try {
      dn = new Intl.DisplayNames([i18n.language], { type: 'currency' })
    } catch {
      dn = undefined
    }
    return CURRENCIES.map((c) => ({ value: c, label: `${c} · ${dn?.of(c) ?? c}` }))
  }, [i18n.language])
}

/**
 * 贸易字段选项：value 存的永远是行业通用英文（落库 + 同步阿里 + 官网展示都用它），
 * 界面文案在译文里给了中文名时显示「value · 中文名」，随界面语言变。
 * 译文缺失或与值相同（英文界面）就只显示英文值，不会出现「T/T · T/T」。
 */
function useEnumOptions(values: readonly string[], keyPrefix: string): Option[] {
  const { t, i18n } = useTranslation()
  return useMemo(
    () =>
      values.map((v) => {
        const local = t(`${keyPrefix}.${v}`, { defaultValue: v })
        return { value: v, label: local && local !== v ? `${v} · ${local}` : v }
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- values/keyPrefix 是模块级常量，t 随语言变化已覆盖重算时机
    [t, i18n.language, keyPrefix, values],
  )
}

const useUnitOptions = () => useEnumOptions(UNITS, 'plugin_website.unit')
const usePortOptions = () => useEnumOptions(PORTS, 'plugin_website.port')
const usePaymentTermOptions = () => useEnumOptions(PAYMENT_TERMS, 'plugin_website.payment')

/** 枚举字段下拉（可搜）：币种/装运港口/付款方式不再手写。
 * 多选时按「逗号+空格」拼接回原字符串，与老数据的 "T/T, L/C" 存储格式互通；
 * 存量手写值不在列表里时补进选项，编辑老数据不会丢显示。 */
function ConstantPicker({
  value,
  onChange,
  options,
  multiple,
  placeholder,
  disabled,
  className,
}: {
  value: string
  onChange: (v: string) => void
  options: Option[]
  multiple?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const selected = useMemo(() => value.split(',').map((s) => s.trim()).filter(Boolean), [value])
  const all = useMemo(() => {
    const list = [...options]
    for (const v of selected) {
      if (!list.some((o) => o.value === v)) list.push({ value: v, label: v })
    }
    return list
  }, [options, selected])
  const kw = search.trim().toLowerCase()
  const shown = kw ? all.filter((o) => o.label.toLowerCase().includes(kw) || o.value.toLowerCase().includes(kw)) : all
  const pick = (v: string) => {
    if (multiple) {
      onChange((selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v]).join(', '))
    } else {
      onChange(v)
      setOpen(false)
    }
  }
  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={placeholder ?? t('common.pleaseSelect')}
      trigger={
        <PickerTrigger
          className={className}
          disabled={disabled}
          placeholder={placeholder}
          hasValue={selected.length > 0}
          allowClear={false}
          display={selected.map((v) => all.find((o) => o.value === v)?.label ?? v).join(', ')}
        />
      }
    >
      <OptionList options={shown} value={selected} multiple={multiple} onSelect={pick} search={search} onSearchChange={setSearch} />
    </PickerShell>
  )
}

/**
 * 产品类目多选（可搜）：与付款方式同款的弹出选择器，收起时只占一行，
 * 展开才见树形清单（缩进表层级、右侧商品数）；搜索命中时连同祖先链一起保留，层级不丢。
 */
function CategoryPicker({
  value, onChange, categories, disabled, className,
}: {
  value: string[]
  onChange: (ids: string[]) => void
  categories: CatNode[]
  disabled?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const byId = useMemo(() => new Map(categories.map((c) => [c.id, c])), [categories])
  const kw = search.trim().toLowerCase()
  const shown = useMemo(() => {
    if (!kw) return categories
    const keep = new Set<string>()
    for (const c of categories) {
      if (c.title.toLowerCase().includes(kw)) {
        keep.add(c.id)
        for (
          let p = c.parentId && c.parentId !== '0' ? byId.get(c.parentId) : undefined;
          p;
          p = p.parentId && p.parentId !== '0' ? byId.get(p.parentId) : undefined
        ) {
          keep.add(p.id)
        }
      }
    }
    return categories.filter((c) => keep.has(c.id))
  }, [categories, byId, kw])
  const options: Option[] = shown.map((c) => ({
    value: c.id,
    label: c.title,
    node: (
      <span className="flex w-full items-center gap-2">
        <span className="truncate" style={{ paddingInlineStart: `${(c.depth - 1) * 16}px` }}>{c.title}</span>
        <span className="num ms-auto text-xs text-muted-foreground">{c.productCount}</span>
      </span>
    ),
  }))
  const toggle = (id: string) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id])
  // 触发器按树形顺序显示已选名称，与清单里的视觉一致。
  const selectedTitles = categories.filter((c) => value.includes(c.id)).map((c) => c.title)
  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={t('plugin_website.product.categories')}
      trigger={
        <PickerTrigger
          className={className}
          disabled={disabled}
          placeholder={t('plugin_website.product.categoriesPlaceholder')}
          hasValue={value.length > 0}
          allowClear={false}
          display={selectedTitles.join('、')}
        />
      }
    >
      <OptionList
        options={options}
        value={value}
        multiple
        onSelect={toggle}
        search={search}
        onSearchChange={setSearch}
        emptyText={t(kw ? 'plugin_website.product.categoryNoMatch' : 'plugin_website.product.categoriesEmpty')}
      />
    </PickerShell>
  )
}

/** 图集：宫格图块（批量直传、拖拽排序、点块看大图、悬停删除），上限与后端 maxImagesPerProduct 对齐；
 * 「从文件库选择」另开弹窗勾选已有图片，关弹窗统一追加（上限在打开时就锁死，不会超）。 */
function ImagesField({
  images, onChange, disabled,
}: {
  images: string[]
  onChange: (images: string[]) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [pickOpen, setPickOpen] = useState(false)
  const [pickedUrls, setPickedUrls] = useState<string[]>([])
  const atLimit = images.length >= IMAGES_MAX
  const room = IMAGES_MAX - images.length
  const openPick = () => {
    setPickedUrls([])
    setPickOpen(true)
  }
  // 关弹窗才算提交：文件库勾选的 URL 追加进图集（不覆盖已有的）。
  const closePick = (open: boolean) => {
    if (!open && pickedUrls.length) onChange([...images, ...pickedUrls])
    setPickOpen(open)
  }
  // RemoteSelect 多选回传的是完整数组（它内部已做过勾选切换），这里只做字符串化、去重并按剩余额度截断
  const togglePick = (v: string | string[]) => {
    const incoming = (Array.isArray(v) ? v : [String(v ?? '')]).filter(Boolean)
    setPickedUrls((urls) => {
      const next = [...urls]
      for (const u of incoming) {
        if (!next.includes(u) && next.length < room) next.push(u)
      }
      return next
    })
  }
  return (
    <div className="flex flex-col gap-2">
      <Uploader
        kind="image"
        visibility="public"
        multiple
        maxCount={IMAGES_MAX}
        disabled={disabled}
        value={images.map((url) => ({
          id: url,
          url,
          name: decodeURIComponent(url.split('/').pop() ?? url),
          size: 0,
          type: 'image/*',
        }))}
        onChange={(files) => onChange(files.map((f) => f.url))}
      />
      {atLimit ? (
        <span className="text-xs text-muted-foreground">{t('plugin_website.product.imageLimit', { max: IMAGES_MAX })}</span>
      ) : (
        <Button variant="link" size="sm" className="h-auto w-fit p-0" disabled={disabled} onClick={openPick}>
          {t('uploader.pickFromLibrary')}
        </Button>
      )}
      <ResponsiveDialog
        open={pickOpen}
        onOpenChange={closePick}
        size="md"
        title={t('uploader.pickFromLibrary')}
        description={t('plugin_website.product.imageLimit', { max: IMAGES_MAX })}
      >
        <RemoteSelect
          url="/files"
          params={{ category: 'image' }}
          valueKey="url"
          labelKey="name"
          multiple
          value={pickedUrls}
          onChange={togglePick}
          allowClear={false}
          disabled={pickedUrls.length >= room}
        />
      </ResponsiveDialog>
    </div>
  )
}

/** 动态属性编辑器：对标阿里的类目属性，label/value 成对增删（服务端按 JSON 文本列存）。 */
function SpecsEditor({
  specs, onChange, disabled,
}: {
  specs: ProductSpec[]
  onChange: (specs: ProductSpec[]) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-col gap-2">
      {specs.map((sp, i) => (
        <div key={i} className="flex items-center gap-2">
          <Input
            value={sp.label}
            maxLength={100}
            disabled={disabled}
            onChange={(e) => onChange(specs.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))}
            placeholder={t('plugin_website.product.specLabel')}
            aria-label={t('plugin_website.product.specLabel')}
            className="w-40"
          />
          <Input
            value={sp.value}
            maxLength={500}
            disabled={disabled}
            onChange={(e) => onChange(specs.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))}
            placeholder={t('plugin_website.product.specValue')}
            aria-label={t('plugin_website.product.specValue')}
            className="min-w-0 flex-1"
          />
          <Button
            variant="ghost"
            size="icon"
            className="text-destructive"
            disabled={disabled}
            aria-label={t('common.delete')}
            onClick={() => onChange(specs.filter((_, j) => j !== i))}
          >
            <X aria-hidden="true" />
          </Button>
        </div>
      ))}
      <Button
        variant="link"
        size="sm"
        className="h-auto w-fit p-0"
        disabled={disabled}
        onClick={() => onChange([...specs, { label: '', value: '' }])}
      >
        <Plus aria-hidden="true" />
        {t('plugin_website.product.specAdd')}
      </Button>
    </div>
  )
}

/**
 * 表单主体：整份表单只有一套，切语言换的是数据源。
 * 可翻译字段（标题/关键词/摘要/SEO/详情）交给 LocaleFormProvider；
 * 图片、价格与类目这类共用字段留在本地 state，译文态下置灰。
 */
function ProductFormBody({
  onOpenChange, editing,
}: {
  onOpenChange: (v: boolean) => void
  editing: SiteProduct | null
}) {
  const { t } = useTranslation()
  const save = useSaveProduct()
  const { data: categories = [] } = useProductCategories()
  const lf = useLocaleForm()
  const { disabled: sharedDisabled } = useSharedField()
  const [slug, setSlug] = useState(editing?.slug ?? '')
  const [coverUrl, setCoverUrl] = useState(editing?.coverUrl ?? '')
  const [images, setImages] = useState<string[]>(() => safeParseArray(editing?.imagesJson))
  const [videoUrl, setVideoUrl] = useState(editing?.videoUrl ?? '')
  const [priceMin, setPriceMin] = useState(editing?.priceMin ? String(editing.priceMin) : '')
  const [priceMax, setPriceMax] = useState(editing?.priceMax ? String(editing.priceMax) : '')
  const [currency, setCurrency] = useState(editing?.currency ?? 'USD')
  const currencyOptions = useCurrencyOptions()
  const unitOptions = useUnitOptions()
  const [priceUnit, setPriceUnit] = useState(editing?.priceUnit ?? '')
  const [moq, setMoq] = useState(editing?.moq ? String(editing.moq) : '')
  const [leadTimeDays, setLeadTimeDays] = useState(editing?.leadTimeDays ? String(editing.leadTimeDays) : '')
  const [port, setPort] = useState(editing?.port ?? '')
  const [paymentTerms, setPaymentTerms] = useState(editing?.paymentTerms ?? '')
  const portOptions = usePortOptions()
  const paymentTermOptions = usePaymentTermOptions()
  const [specs, setSpecs] = useState<ProductSpec[]>(() => safeParseSpecs(editing?.specsJson))
  const [seoKeywords, setSeoKeywords] = useState(editing?.seoKeywords ?? '')
  const [noindex, setNoindex] = useState(editing?.noindex ?? false)
  const [author, setAuthor] = useState(editing?.author ?? '')
  const [catIds, setCatIds] = useState<string[]>(() => (editing?.categories ?? []).map((c) => c.id))
  const flatCats = useMemo(() => flattenCatTree(buildCatTree(categories)), [categories])
  // slug 是所有语言共用的，但每门语言有各自的路径前缀；提示按当前编辑的语言拼。
  const pathPrefix = lf.locales.find((l) => l.code === lf.active)?.prefix
  const productPath = `${pathPrefix ? `/${pathPrefix}` : ''}/products/${slug || '…'}`
  // 站内相对路径或绝对地址；别的一律拦住，否则存下去就是死链（公开页还有 og:image 跟着指向它）。
  const coverInvalid = coverUrl.trim() !== '' && !/^(https?:\/\/|\/)/i.test(coverUrl.trim())
  const online = lf.publishedOf(lf.defaultLocale)

  const submit = async () => {
    const base = lf.baseValues()
    if (!(base.title ?? '').trim()) {
      toast.error(t('plugin_website.product.required'))
      return
    }
    if (coverInvalid) {
      toast.error(t('plugin_website.fields.coverInvalid'))
      return
    }
    // 英文母本校验（对齐阿里）：配置了英文且英文不是默认语言时，上架前必须先填英文标题——
    // 二期同步阿里直接取英文内容，没有母本同步就是空发。
    if (online) {
      const en = lf.locales.find((l) => l.code.toLowerCase().startsWith('en'))
      if (en && en.code !== lf.defaultLocale) {
        let enTitle = ''
        try {
          const doc = JSON.parse(lf.serialize().i18nJson || '{}') as Record<string, Record<string, string>>
          enTitle = (doc[en.code]?.title ?? '').trim()
        } catch {
          // serialize 出来的 JSON 不会坏；坏了就当没填，走下面的拦截
        }
        if (!enTitle) {
          toast.error(t('plugin_website.product.englishTitleRequired'))
          lf.setActive(en.code)
          return
        }
      }
    }
    try {
      await save.mutateAsync({
        id: editing?.id,
        data: {
          // slug 留空 = 服务端按标题走 pkg/slug 行业标准生成；前端预填同一算法只是省一次输入
          slug: slug.trim() || slugify(base.title ?? ''),
          coverUrl,
          images: images.map((img) => img.trim()).filter(Boolean),
          videoUrl,
          priceMin: parseNum(priceMin),
          priceMax: parseNum(priceMax),
          currency,
          priceUnit,
          moq: parseNum(moq),
          leadTimeDays: parseNum(leadTimeDays),
          port,
          paymentTerms,
          specs: specs.filter((sp) => sp.label.trim() || sp.value.trim()),
          categoryIds: catIds,
          noindex,
          author,
          seoKeywords,
          status: online ? 'online' : 'draft',
          // 只有上架态才触发英文母本校验；publishedJson 的按语言发布与新闻同机制
          publishedJson: lf.serialize().publishedJson,
          i18nJson: lf.serialize().i18nJson,
          // 行上的裸列永远存原文
          title: base.title ?? '',
          keywords: base.keywords ?? '',
          summary: base.summary ?? '',
          seoTitle: base.seoTitle ?? '',
          seoDesc: base.seoDesc ?? '',
          content: base.content ?? '',
        },
      })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={editing ? t('plugin_website.product.edit') : t('plugin_website.product.create')}
      size="lg"
      className="space-y-4"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button onClick={() => void submit()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="flex items-center justify-between gap-2 pb-1">
        <LocaleSwitcher />
        <span className="text-xs text-muted-foreground">{t('plugin_website.i18n.switchHint')}</span>
      </div>
      <div className="flex flex-col gap-5">
        <FormSection title={t('plugin_website.product.basicSection')}>
          <LocalizedField fieldKey="title" required />
          <LocalizedField fieldKey="keywords" aiTarget="keywords" placeholder={t('plugin_website.fields.productKeywordsPlaceholder')} />
          <KeywordHint value={lf.field('keywords')} />
          <LocalizedField fieldKey="summary" />
          <div className="grid gap-3 md:grid-cols-2">
            <SharedFieldShell labelKey="plugin_website.fields.slug">
              <Input
                maxLength={64}
                value={slug}
                disabled={sharedDisabled}
                onChange={(e) => setSlug(e.target.value)}
                placeholder={t('plugin_website.fields.slugAuto')}
                className="font-mono"
              />
              <span className="truncate text-xs text-muted-foreground">{productPath}</span>
            </SharedFieldShell>
            <SharedFieldShell labelKey="plugin_website.product.categories">
              <CategoryPicker value={catIds} onChange={setCatIds} categories={flatCats} disabled={sharedDisabled} />
            </SharedFieldShell>
          </div>
        </FormSection>
        <FormSection title={t('plugin_website.product.imageSection')}>
          <div className="grid gap-3 md:grid-cols-2">
            <SharedFieldShell labelKey="plugin_website.fields.coverUrl">
              <ImageUrlField
                value={coverUrl}
                onChange={setCoverUrl}
                disabled={sharedDisabled}
                title={t('plugin_website.product.coverUpload')}
                hint={t('plugin_website.product.coverHint')}
                crop={{ width: 800, height: 800 }}
                previewClassName="h-20 w-20 object-cover p-0"
                aria-label={t('plugin_website.fields.coverUrl')}
              />
              {coverInvalid ? (
                <span className="text-xs text-destructive">{t('plugin_website.fields.coverInvalid')}</span>
              ) : null}
            </SharedFieldShell>
            <SharedFieldShell labelKey="plugin_website.product.videoUrl">
              <VideoUrlField
                value={videoUrl}
                onChange={setVideoUrl}
                disabled={sharedDisabled}
                title={t('plugin_website.product.videoUrl')}
                hint={t('plugin_website.product.videoHint')}
                placeholder="https://…"
                aria-label={t('plugin_website.product.videoUrl')}
              />
            </SharedFieldShell>
          </div>
          <SharedGroup labelKey="plugin_website.product.images">
            <ImagesField images={images} onChange={setImages} disabled={sharedDisabled} />
          </SharedGroup>
        </FormSection>
        <FormSection title={t('plugin_website.product.tradeSection')}>
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
          <SharedFieldShell labelKey="plugin_website.product.priceMin">
            <Input
              type="number"
              min="0"
              step="0.01"
              value={priceMin}
              disabled={sharedDisabled}
              onChange={(e) => setPriceMin(e.target.value)}
            />
          </SharedFieldShell>
          <SharedFieldShell labelKey="plugin_website.product.priceMax">
            <Input
              type="number"
              min="0"
              step="0.01"
              value={priceMax}
              disabled={sharedDisabled}
              onChange={(e) => setPriceMax(e.target.value)}
            />
          </SharedFieldShell>
          <SharedFieldShell labelKey="plugin_website.product.currency">
            <ConstantPicker
              value={currency}
              onChange={setCurrency}
              options={currencyOptions}
              placeholder="USD"
              disabled={sharedDisabled}
            />
          </SharedFieldShell>
          <SharedFieldShell labelKey="plugin_website.product.priceUnit">
            <ConstantPicker
              value={priceUnit}
              onChange={setPriceUnit}
              options={unitOptions}
              placeholder="piece"
              disabled={sharedDisabled}
            />
          </SharedFieldShell>
          <SharedFieldShell labelKey="plugin_website.product.moq">
            <Input
              type="number"
              min="0"
              value={moq}
              disabled={sharedDisabled}
              onChange={(e) => setMoq(e.target.value)}
            />
          </SharedFieldShell>
          <SharedFieldShell labelKey="plugin_website.product.leadTimeDays">
            <Input
              type="number"
              min="0"
              value={leadTimeDays}
              disabled={sharedDisabled}
              onChange={(e) => setLeadTimeDays(e.target.value)}
            />
          </SharedFieldShell>
        </div>
        <div className="grid gap-3 md:grid-cols-2">
          <SharedFieldShell labelKey="plugin_website.product.port">
            <ConstantPicker
              value={port}
              onChange={setPort}
              options={portOptions}
              multiple
              placeholder={t('plugin_website.product.portPlaceholder')}
              disabled={sharedDisabled}
            />
          </SharedFieldShell>
          <SharedFieldShell labelKey="plugin_website.product.paymentTerms">
            <ConstantPicker
              value={paymentTerms}
              onChange={setPaymentTerms}
              options={paymentTermOptions}
              multiple
              placeholder="T/T, L/C"
              disabled={sharedDisabled}
            />
          </SharedFieldShell>
        </div>
        <SharedGroup labelKey="plugin_website.product.specs">
          <SpecsEditor specs={specs} onChange={setSpecs} disabled={sharedDisabled} />
        </SharedGroup>
        </FormSection>
        <FormSection title={t('plugin_website.product.detailSection')}>
          <LocalizedField fieldKey="content" className="min-h-40" />
          <div className="grid gap-3 md:grid-cols-2">
            <LocalizedField fieldKey="seoTitle" />
            <LocalizedField fieldKey="seoDesc" />
          </div>
          <LocaleFormActions />
          <SharedFieldShell labelKey="plugin_website.fields.productSeoKeywords">
            <Input
              value={seoKeywords}
              maxLength={300}
              disabled={sharedDisabled}
              onChange={(e) => setSeoKeywords(e.target.value)}
              placeholder={t('plugin_website.fields.seoKeywords')}
            />
          </SharedFieldShell>
        </FormSection>
        <FormSection title={t('plugin_website.product.publishSection')}>
          <div className="grid gap-3 md:grid-cols-3">
            <SwitchRow
              label={t('plugin_website.fields.published')}
              checked={online}
              onCheckedChange={(on) => lf.setPublishedOf(lf.defaultLocale, on)}
            />
            <SwitchRow
              label={t('plugin_website.fields.noindex')}
              checked={noindex}
              onCheckedChange={setNoindex}
            />
            <SharedFieldShell labelKey="plugin_website.fields.author">
              <Input
                value={author}
                maxLength={64}
                disabled={sharedDisabled}
                onChange={(e) => setAuthor(e.target.value)}
              />
            </SharedFieldShell>
          </div>
        </FormSection>
      </div>
    </ResponsiveDialog>
  )
}

/** 表单分区：竖色条标题 + 延伸分隔线，长表单的视觉锚点（替代裸的加粗一行）。 */
function FormSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <span className="h-4 w-1 shrink-0 rounded-full bg-primary" aria-hidden="true" />
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
        <span className="h-px min-w-4 flex-1 bg-border" aria-hidden="true" />
      </div>
      {children}
    </section>
  )
}

/** 分组外壳：与 SharedFieldShell 同款标签行，但外层是 div（组里第一颗控件不能被 label 认领）。 */
function SharedGroup({ labelKey, children }: { labelKey: string; children: ReactNode }) {
  const { t } = useTranslation()
  const { isSource } = useLocaleForm()
  return (
    <div className="flex flex-col gap-1 text-sm">
      <span className={cn('inline-flex min-h-7 items-center gap-1.5', !isSource && 'text-muted-foreground')}>
        <span className="inline-flex items-baseline">{t(labelKey)}</span>
        {!isSource ? (
          <span className="rounded border px-1 text-[10px] font-semibold">{t('localeForm.sharedTag')}</span>
        ) : null}
      </span>
      {children}
    </div>
  )
}

/**
 * 阿里开放平台凭据弹窗：AppKey 明文，Secret/SessionKey 密码框（已配置时占位提示「留空保持不变」）。
 * 保存走独立 PUT（凭据三态在后端处理），连接测试调 category 接口验证网关+签名。
 * 入口是页头的「阿里同步配置」按钮：凭据是一次性配置，不该常驻占掉列表首屏；
 * 按钮上常显「未配置」徽标，缺什么一目了然（原折叠卡的自动展开在此等价为按钮徽标提示）。
 */
function AliConfigDialog({ onOpenChange }: { onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const { data: config, isLoading } = useAliConfig()
  const save = useSaveAliConfig()
  const [appKey, setAppKey] = useState('')
  const [appSecret, setAppSecret] = useState('')
  const [sessionKey, setSessionKey] = useState('')
  const [apiGateway, setApiGateway] = useState('')
  const [testState, setTestState] = useState<'idle' | 'testing' | 'ok' | 'fail'>('idle')
  // 配置到货时回填非敏感字段一次（后续用户编辑不覆盖）。
  const [loaded, setLoaded] = useState<string>('')
  if (config && loaded !== config.appKey) {
    setLoaded(config.appKey)
    setAppKey(config.appKey)
    setApiGateway(config.apiGateway)
  }

  const submit = async () => {
    try {
      await save.mutateAsync({
        appKey,
        appSecret,
        sessionKey,
        apiGateway,
      })
      setAppSecret('')
      setSessionKey('')
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 服务端原因由 http 拦截器统一弹出
    }
  }

  const test = async () => {
    setTestState('testing')
    try {
      await fetchAliTest()
      setTestState('ok')
      toast.success(t('plugin_website.ali.testOk'))
    } catch {
      setTestState('fail')
      toast.error(t('plugin_website.ali.testFail'))
    }
  }

  if (isLoading) return null
  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={t('plugin_website.ali.configTitle')}
      description={t('plugin_website.ali.configDesc')}
      size="md"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button onClick={() => void submit()} disabled={save.isPending}>
            {t('plugin_website.ali.save')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-sm">
        {/* 凭据三态常显：Secret/SessionKey 配没配，保存/测试结果各一枚徽标 */}
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={config?.hasSecret ? 'success' : 'warning'}>
            {config?.hasSecret ? t('plugin_website.ali.secretConfigured') : t('plugin_website.ali.secretMissing')}
          </Badge>
          <Badge variant={config?.hasSession ? 'success' : 'warning'}>
            {config?.hasSession ? t('plugin_website.ali.sessionConfigured') : t('plugin_website.ali.sessionMissing')}
          </Badge>
        </div>
        <div className="grid grid-cols-1 gap-2 md:grid-cols-3">
          <Input
            value={appKey}
            onChange={(e) => setAppKey(e.target.value)}
            placeholder={t('plugin_website.ali.appKey')}
            aria-label={t('plugin_website.ali.appKey')}
            // 这个弹窗形似登录表单（多个输入 + 密码框），浏览器会把站内账号密码自动填进来：
            // 截图里的 AppKey=admin、AppSecret=10 个圆点就是这么来的。Chrome 的自动填充底色是冷蓝，
            // 两个框因此比同排其它控件亮一档；更糟的是点「保存凭据」会把这串站点密码当成
            // AppSecret 加密存库。new-password 是 Chrome 唯一遵从而不填充的写法。
            autoComplete="off"
            name="ali-app-key"
          />
          <Input
            type="password"
            value={appSecret}
            onChange={(e) => setAppSecret(e.target.value)}
            placeholder={config?.hasSecret ? t('plugin_website.ali.configuredAlready') : t('plugin_website.ali.appSecret')}
            aria-label={t('plugin_website.ali.appSecret')}
            autoComplete="new-password"
            name="ali-app-secret"
          />
          <Input
            type="password"
            value={sessionKey}
            onChange={(e) => setSessionKey(e.target.value)}
            placeholder={config?.hasSession ? t('plugin_website.ali.configuredAlready') : t('plugin_website.ali.sessionKey')}
            aria-label={t('plugin_website.ali.sessionKey')}
            autoComplete="new-password"
            name="ali-session-key"
          />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Input
            value={apiGateway}
            onChange={(e) => setApiGateway(e.target.value)}
            placeholder="https://gw.api.taobao.com/router/rest"
            aria-label={t('plugin_website.ali.apiGateway')}
            className="min-w-0 flex-1 font-mono text-xs md:w-96 md:flex-none"
            autoComplete="off"
            name="ali-api-gateway"
          />
          <Button variant="outline" size="sm" onClick={() => void test()} disabled={testState === 'testing'}>
            {testState === 'testing' ? t('common.loading') : t('plugin_website.ali.test')}
          </Button>
          {testState === 'ok' ? <Badge variant="success">{t('plugin_website.ali.testOk')}</Badge> : null}
          {testState === 'fail' ? <Badge variant="destructive">{t('plugin_website.ali.testFail')}</Badge> : null}
        </div>
      </div>
    </ResponsiveDialog>
  )
}

/**
 * 产品导入弹窗（页内完成，不跳导入中心）：模板下载、文件上传、进度与逐行失败清单。
 * 执行走通用导入中心接口（source=website_products）：行数少同步完成，多时后台跑、这里轮询。
 */
function ProductImportDialog({ onOpenChange }: { onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const run = useRunProductImport()
  const [file, setFile] = useState<File | null>(null)
  const [job, setJob] = useState<ProductImportJob | null>(null)
  const running = job?.status === 'pending' || job?.status === 'running'
  // 后台任务未结束前每 2s 轮询台账；结束时把最终数据换上（错误明细随单条任务返回）。
  useEffect(() => {
    if (!running || !job) return
    const timer = setInterval(() => {
      void fetchProductImportJob(job.id)
        .then(setJob)
        .catch(() => undefined)
    }, 2000)
    return () => clearInterval(timer)
  }, [job, running])

  const downloadTemplate = async () => {
    try {
      const blob = await fetchProductImportTemplate()
      saveBlob(`${t('plugin_website.product.import.templateFilename')}.csv`, blob)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const start = async () => {
    if (!file) return
    try {
      setJob(await run.mutateAsync(file))
    } catch {
      // 服务端原因（文件不合法、无导入权限等）由 http 拦截器统一弹出
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      size="md"
      title={t('plugin_website.product.import.title')}
      description={t('plugin_website.product.import.description')}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.close')}</Button>
          <Button onClick={() => void start()} disabled={!file || run.isPending || running}>
            {run.isPending || running ? t('common.loading') : t('plugin_website.product.import.start')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-1.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Label htmlFor="product-import-file">{t('plugin_website.product.import.file')}</Label>
            <Button type="button" variant="ghost" size="sm" className="-me-2 text-muted-foreground" onClick={() => void downloadTemplate()}>
              <Download aria-hidden="true" />
              {t('plugin_website.product.import.template')}
            </Button>
          </div>
          <FileDropField
            accept=".xlsx,.csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv"
            labelledBy="product-import-file"
            files={file ? [file] : []}
            onChange={(fs) => {
              setFile(fs[0] ?? null)
              setJob(null)
            }}
          />
          <p className="text-xs leading-relaxed text-muted-foreground">{t('plugin_website.product.import.fileHint')}</p>
        </div>
        {job ? (
          <div className="flex flex-col gap-2.5 rounded-lg border border-border bg-muted/50 p-3" role="status">
            <p className="flex items-center gap-2 text-sm font-medium text-foreground">
              {running ? (
                <span className="size-4 shrink-0 animate-spin rounded-full border-2 border-current border-t-transparent" aria-hidden="true" />
              ) : (job.errors?.length ?? 0) > 0 || job.status === 'failed' ? (
                <CircleAlert className="size-4 shrink-0 text-warning" aria-hidden="true" />
              ) : (
                <CircleCheck className="size-4 shrink-0 text-success" aria-hidden="true" />
              )}
              {running
                ? t('plugin_website.product.import.running')
                : job.status === 'failed'
                  ? t('plugin_website.product.import.failed')
                  : t('plugin_website.product.import.result', { ok: job.okRows, failed: job.failRows })}
            </p>
            {!running && (job.errors?.length ?? 0) > 0 ? (
              <ul className="flex max-h-48 flex-col gap-1 overflow-y-auto text-sm text-destructive">
                {job.errors!.map((e) => (
                  <li key={e.row}>{t('plugin_website.product.import.errorRow', { row: e.row, message: e.message })}</li>
                ))}
              </ul>
            ) : null}
          </div>
        ) : null}
      </div>
    </ResponsiveDialog>
  )
}

/** 类目浏览状态：面包屑路径（push 进入子级，pop 返回）。 */
interface CatPathEntry {
  id: string
  label: string
}

/**
 * 阿里类目树选择器：按需展开拉子级，叶子才可选。
 * current 列表用 useQuery 缓存（key=父 ID），切语言/重开弹窗不重复拉。
 */
function AliCategoryPicker({
  selected, onSelect, path, onPathChange,
}: {
  selected: AliCategoryNode | null
  onSelect: (node: AliCategoryNode | null) => void
  path: CatPathEntry[]
  onPathChange: (path: CatPathEntry[]) => void
}) {
  const { t } = useTranslation()
  const parent = path.length ? path[path.length - 1].id : '0'
  const { data: nodes = [], isLoading } = useQuery({
    queryKey: ['plugin_website', 'ali-categories', parent],
    queryFn: () => fetchAliCategories(parent),
  })
  const enter = (node: AliCategoryNode) => {
    onPathChange([...path, { id: node.id, label: node.cnName || node.name }])
    onSelect(null)
  }
  const rootLabel = t('plugin_website.ali.categoryRoot')
  return (
    <div className="rounded-md border border-border">
      <div className="flex flex-wrap items-center gap-1 border-b border-border px-2 py-1.5 text-xs text-muted-foreground">
        <button type="button" className="hover:text-foreground" onClick={() => onPathChange([])}>
          {rootLabel}
        </button>
        {path.map((p, i) => (
          <span key={p.id} className="inline-flex items-center gap-1">
            <span aria-hidden="true">/</span>
            {i === path.length - 1 ? (
              <span className="text-foreground">{p.label}</span>
            ) : (
              <button type="button" className="hover:text-foreground" onClick={() => onPathChange(path.slice(0, i + 1))}>
                {p.label}
              </button>
            )}
          </span>
        ))}
      </div>
      <div className="max-h-56 overflow-y-auto">
        {isLoading ? <p className="px-2 py-3 text-xs text-muted-foreground">{t('common.loading')}</p> : null}
        {!isLoading && !nodes.length ? <p className="px-2 py-3 text-xs text-muted-foreground">{t('plugin_website.ali.categoryEmpty')}</p> : null}
        {nodes.map((n) => (
          <button
            key={n.id}
            type="button"
            className={cn(
              'flex min-h-9 w-full items-center gap-2 px-2 text-start text-sm hover:bg-accent/60',
              selected?.id === n.id && 'bg-accent/60',
            )}
            onClick={() => (n.leaf ? onSelect(n) : enter(n))}
          >
            <span className="min-w-0 flex-1 truncate">{n.cnName || n.name}</span>
            {n.leaf ? (
              <Badge variant="success">{t('plugin_website.ali.categoryLeaf')}</Badge>
            ) : (
              <span className="text-xs text-muted-foreground" aria-hidden="true">›</span>
            )}
          </button>
        ))}
      </div>
      <p className="border-t border-border px-2 py-1.5 text-xs text-muted-foreground">{t('plugin_website.ali.categoryLeafHint')}</p>
    </div>
  )
}

/** 同步对话框目标：单个商品或一批。 */
export interface AliSyncTarget {
  mode: 'single' | 'batch'
  product?: SiteProduct
  ids?: string[]
}

/**
 * 同步到阿里对话框：选阿里叶子类目（选中即回填商品）+ 发品类型，确认后执行。
 * 单个同步等待结果（成功带阿里商品 ID）；批量提交 job 后切换到进度视图（轮询 ali/sync-status）。
 */
function AliSyncDialog({ target, onOpenChange }: { target: AliSyncTarget; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const bindCategory = useBindAliCategory()
  const syncOne = useSyncProduct()
  const syncBatch = useSyncProductBatch()
  const [market, setMarket] = useState<AliMarket>('inquiry')
  const [selected, setSelected] = useState<AliCategoryNode | null>(null)
  const [path, setPath] = useState<CatPathEntry[]>([])
  const [jobId, setJobId] = useState<string | null>(null)
  const { data: jobStatus } = useAliSyncStatus(jobId ?? undefined)

  const count = target.mode === 'single' ? 1 : (target.ids?.length ?? 0)
  const busy = syncOne.isPending || syncBatch.isPending || (!!jobId && (jobStatus?.status === 'queued' || jobStatus?.status === 'running'))
  const done = !!jobStatus && (jobStatus.status === 'success' || jobStatus.status === 'failed' || jobStatus.status === 'canceled')

  const confirm = async () => {
    if (!selected) {
      toast.error(t('plugin_website.ali.categoryRequired'))
      return
    }
    if (target.mode === 'single' && target.product) {
      try {
        if (target.product.alibabaCategoryId !== selected.id) {
          await bindCategory.mutateAsync({ id: target.product.id, categoryId: selected.id })
        }
        const res = await syncOne.mutateAsync({ id: target.product.id, market })
        toast.success(t('plugin_website.ali.syncDone', { id: res.alibabaProductId }))
        onOpenChange(false)
      } catch (err) {
        // 后端已把失败原因写回商品行；这里把报错读出来 toast（silent 请求不走全局拦截器）
        const msg = err instanceof Error ? err.message : ''
        toast.error(msg || t('plugin_website.ali.syncFailed'))
      }
      return
    }
    try {
      const res = await syncBatch.mutateAsync({ ids: target.ids ?? [], market })
      if (selected && target.ids?.length) {
        // 批量也把所选类目回填到每个商品上（后台逐个同步时按它发品）
        await Promise.all(target.ids.map((id) => bindCategory.mutateAsync({ id, categoryId: selected.id }).catch(() => null)))
      }
      toast.success(t('plugin_website.ali.syncSubmitted'))
      setJobId(res.jobId)
    } catch (err) {
      const msg = err instanceof Error ? err.message : ''
      toast.error(msg || t('plugin_website.ali.syncFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={t('plugin_website.ali.syncTitle')}
      description={target.mode === 'single'
        ? t('plugin_website.ali.syncOneHint', { name: target.product?.title ?? '' })
        : t('plugin_website.ali.syncCount', { count })}
      size="md"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          {jobId && !done ? (
            <Button disabled>{t('common.loading')}</Button>
          ) : (
            <Button onClick={() => void confirm()} disabled={busy}>{t('plugin_website.ali.confirm')}</Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-3 text-sm">
        {jobId ? (
          <div className="flex flex-col gap-1 rounded-md border border-border p-3">
            <p className="font-medium">{t('plugin_website.ali.progressTitle')}</p>
            <p className="num text-xs text-muted-foreground">{jobStatus?.message || t('common.loading')}</p>
            {done && jobStatus?.status === 'failed' ? (
              <p className="text-xs text-destructive">{jobStatus.error}</p>
            ) : null}
            {done ? <p className="text-xs text-muted-foreground">{t('plugin_website.ali.jobDone')}</p> : null}
          </div>
        ) : (
          <>
            <div>
              <p className="mb-1">{t('plugin_website.ali.category')}</p>
              <AliCategoryPicker selected={selected} onSelect={setSelected} path={path} onPathChange={setPath} />
              {selected ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  {t('plugin_website.ali.selectedCategory', { name: selected.cnName || selected.name, id: selected.id })}
                </p>
              ) : target.mode === 'single' && target.product?.alibabaCategoryId ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  {t('plugin_website.ali.boundCategory', { id: target.product.alibabaCategoryId })}
                </p>
              ) : null}
            </div>
            <div>
              <p className="mb-1">{t('plugin_website.ali.market')}</p>
              <Select value={market} onValueChange={(v) => setMarket(v as AliMarket)}>
                <SelectTrigger className="w-full" aria-label={t('plugin_website.ali.market')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="inquiry">{t('plugin_website.ali.marketInquiry')}</SelectItem>
                  <SelectItem value="wholesale">{t('plugin_website.ali.marketWholesale')}</SelectItem>
                </SelectContent>
              </Select>
              <p className="mt-1 text-xs text-muted-foreground">{t('plugin_website.ali.englishHint')}</p>
            </div>
          </>
        )}
      </div>
    </ResponsiveDialog>
  )
}

function safeParseArray(raw?: string): string[] {
  if (!raw) return []
  try {
    const out = JSON.parse(raw)
    return Array.isArray(out) ? out.map(String) : []
  } catch {
    return []
  }
}

function safeParseSpecs(raw?: string): ProductSpec[] {
  if (!raw) return []
  try {
    const out = JSON.parse(raw)
    if (!Array.isArray(out)) return []
    return out
      .filter((x): x is ProductSpec => !!x && typeof x === 'object')
      .map((x) => ({ label: String(x.label ?? ''), value: String(x.value ?? '') }))
  } catch {
    return []
  }
}

function ProductFormDialog({
  onOpenChange, editing, initialLocale,
}: {
  onOpenChange: (v: boolean) => void
  editing: SiteProduct | null
  initialLocale?: string
}) {
  const { data: languages = [] } = useLanguages()
  const { data: schema, isError, refetch } = useI18nSchema('product')
  const defaultLocale = languages.find((l) => l.default)?.code ?? ''
  // 传输函数交给框架组件：它不认识插件路由，只负责按格式分批调用。
  const translate = useCallback<TranslateBatch>(
    (texts, to, format) => translateContent('product', texts, defaultLocale, to, format),
    [defaultLocale],
  )

  // schema 与语言清单是表单的前提，没到货前不给渲染——否则会闪出一个没有字段的空表单。
  if (!schema || !defaultLocale) {
    return <EditorSchemaGate onOpenChange={onOpenChange} error={isError} onRetry={() => void refetch()} />
  }

  return (
    <LocaleFormProvider
      schema={schema}
      locales={languages}
      defaultLocale={defaultLocale}
      base={{
        title: editing?.title ?? '',
        keywords: editing?.keywords ?? '',
        summary: editing?.summary ?? '',
        seoTitle: editing?.seoTitle ?? '',
        seoDesc: editing?.seoDesc ?? '',
        seoKeywords: editing?.seoKeywords ?? '',
        content: editing?.content ?? '',
      }}
      i18nJson={editing?.i18nJson}
      publishedJson={editing?.publishedJson}
      sourcePublished={editing?.status === 'online'}
      initialLocale={initialLocale}
      translate={translate}
    >
      <ProductFormBody onOpenChange={onOpenChange} editing={editing} />
    </LocaleFormProvider>
  )
}

export default function ProductPage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  // 预取字段 schema：点「新建/编辑」时直接命中缓存，不再先闪一个加载弹窗。
  useI18nSchema('product')
  const { data: languages = [] } = useLanguages()
  const { data: categories = [] } = useProductCategories()
  const flatCats = useMemo(() => flattenCatTree(buildCatTree(categories)), [categories])
  const [draft, setDraft] = useState<ContentDraft>({})
  const [filters, setFilters] = useState<ContentQuery>({})
  // 状态页签（对齐阿里：全部/上架/下架/草稿/审核）与关键词筛选分开：页签是快筛，FilterBar 管关键词与类目。
  const [statusTab, setStatusTab] = useState<string>(ALL)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [selected, setSelected] = useState<string[]>([])
  const effectiveStatus = statusTab === ALL ? filters.status : statusTab
  const { data, isLoading } = useProducts({ ...filters, status: effectiveStatus, page, pageSize })
  const extraLocales = languages.filter((l) => !l.default)
  const remove = useDeleteProduct()
  const removeBatch = useDeleteProducts()
  const setStatus = useSetProductStatus()
  const copy = useCopyProduct()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<SiteProduct | null>(null)
  const [focus, setFocus] = useState<{ code: string; nonce: number } | null>(null)
  // 阿里同步对话框的目标：单个商品或一批；null = 关闭。
  const [syncTarget, setSyncTarget] = useState<AliSyncTarget | null>(null)
  // 阿里凭据配置入口在页头：按钮上带「未配置」徽标（页面与弹窗共用同一个 useAliConfig 缓存）。
  const { data: aliConfig } = useAliConfig()
  const [aliOpen, setAliOpen] = useState(false)
  // 列表缩略图点开看大图：记整行，弹窗同时用 coverUrl 和标题。
  const [preview, setPreview] = useState<SiteProduct | null>(null)

  const openAt = (row: SiteProduct | null, code?: string) => {
    setEditing(row)
    setFocus(code ? { code, nonce: Date.now() } : null)
    setFormOpen(true)
  }

  const onDelete = async (rows: SiteProduct[]) => {
    if (
      await confirm({
        destructive: true,
        description: rows.length === 1
          ? t('plugin_website.product.deleteConfirm', { name: rows[0].title })
          : t('plugin_website.product.deleteBatchConfirm', { count: rows.length }),
      })
    ) {
      try {
        if (rows.length === 1) await remove.mutateAsync(rows[0].id)
        else await removeBatch.mutateAsync(rows.map((r) => r.id))
        setSelected([])
        toast.success(t('common.operateSuccess'))
      } catch {
        // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
      }
    }
  }

  // 批量上架/下线后要清选择：列表一刷新，选中的行可能已经不在了。
  const applyStatus = async (ids: string[], status: 'online' | 'offline') => {
    try {
      await setStatus.mutateAsync({ ids, status })
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    }
  }

  const onPreview = async (row: SiteProduct) => {
    try {
      const url = await fetchProductPreview(row.id)
      window.open(url, '_blank', 'noopener')
    } catch (err) {
      // fetchProductPreview 是 silent 请求，不走全局拦截器：这里不弹的话点预览就是毫无反应
      const msg = err instanceof Error ? err.message : ''
      toast.error(msg || t('common.operateFailed'))
    }
  }

  const onCopy = async (row: SiteProduct) => {
    try {
      const fresh = await copy.mutateAsync(row.id)
      toast.success(t('plugin_website.product.copied', { slug: fresh.slug }))
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    }
  }

  const [exporting, setExporting] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const { start: startExportJob } = useAsyncJob()
  // 导出沿用当前筛选（不是当前页）；勾选了行就只导勾选的（ids 传后端，筛选条件忽略）。
  // 大数据量分流与用户导出同款：先问 export-plan，超过 export.sync_limit 改走异步任务——
  // toast 提示「已提交到导出中心」，进度与 .xlsx 下载都在导出中心看，不阻塞页面。
  const onExport = async () => {
    setExporting(true)
    try {
      const query = selected.length
        ? { ids: selected }
        : { keyword: filters.keyword, status: effectiveStatus, categoryIds: filters.categoryIds }
      const plan = await fetchProductExportPlan(query)
      if (plan?.async) {
        await startExportJob(() => createProductExportJob(query))
        return
      }
      const rows = await fetchProductsForExport(query)
      const stamp = new Date().toISOString().slice(0, 10).replace(/-/g, '')
      downloadCsv(`products-${stamp}.csv`, [
        [
          t('plugin_website.product.titleColumn'),
          t('plugin_website.fields.slug'),
          t('plugin_website.product.categoryColumn'),
          t('plugin_website.product.priceColumn'),
          t('plugin_website.product.moq'),
          t('plugin_website.product.statusColumn'),
          t('plugin_website.product.syncColumn'),
          t('plugin_website.fields.updatedAt'),
        ],
        ...rows.map((r) => [
          r.title,
          r.slug,
          (r.categories ?? []).map((c) => c.title).join('、'),
          priceText(r),
          r.moq || '',
          t(`plugin_website.product.status${r.status.charAt(0).toUpperCase()}${r.status.slice(1)}`, { defaultValue: r.status }),
          syncText(r, t),
          formatTime(r.updatedAt, i18n.language),
        ]),
      ])
      toast.success(t('common.operateSuccess'))
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    } finally {
      setExporting(false)
    }
  }

  const selectedRows = (data?.list ?? []).filter((r) => selected.includes(r.id))

  const columns: Column<SiteProduct>[] = [
    {
      key: 'title',
      title: t('plugin_website.product.titleColumn'),
      mobile: 'title',
      render: (r) => (
        <span className="flex min-w-0 items-center gap-2">
          {r.coverUrl ? (
            <button
              type="button"
              className="flex-none rounded-md focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
              onClick={() => setPreview(r)}
              aria-label={t('common.preview')}
              title={t('common.preview')}
            >
              <img src={r.coverUrl} alt="" className="h-10 w-10 rounded-md border border-border object-cover" />
            </button>
          ) : (
            <span className="flex h-10 w-10 flex-none items-center justify-center rounded-md border border-border text-sm text-muted-foreground">
              {r.title.slice(0, 1).toUpperCase()}
            </span>
          )}
          <span className="min-w-0">
            {/* 点名称直接进编辑：列表页最高频的动作，不用再去行尾找铅笔。 */}
            <button
              type="button"
              className="block w-full max-w-full truncate text-start font-medium text-foreground hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
              onClick={() => openAt(r)}
              title={t('common.edit')}
            >
              {r.title}
            </button>
            <span className="num block text-xs text-muted-foreground">{r.id}</span>
          </span>
        </span>
      ),
    },
    {
      key: 'slug',
      title: t('plugin_website.fields.slug'),
      mobile: 'sub',
      render: (r) => <span className="font-mono text-xs">/products/{r.slug}</span>,
    },
    {
      key: 'categories',
      title: t('plugin_website.product.categoryColumn'),
      render: (r) => {
        const list = r.categories ?? []
        if (!list.length) return <span className="text-muted-foreground">—</span>
        return (
          <div className="flex flex-wrap gap-1">
            {list.map((c) => (
              <Badge key={c.id} variant="outline">{c.title}</Badge>
            ))}
          </div>
        )
      },
    },
    {
      key: 'price',
      title: t('plugin_website.product.priceColumn'),
      mobile: 'hidden',
      render: (r) => {
        const text = priceText(r)
        return text ? <span className="num text-foreground">{text}</span> : <span className="text-xs text-muted-foreground">{t('plugin_website.product.noPrice')}</span>
      },
    },
    {
      key: 'sync',
      title: t('plugin_website.product.syncColumn'),
      mobile: 'hidden',
      render: (r) => (
        <span title={r.syncError || undefined} className="inline-flex">
          {syncBadge(r, t)}
        </span>
      ),
    },
    {
      key: 'i18n',
      title: t('plugin_website.i18n.status'),
      mobile: 'hidden',
      render: (r) => (
        <TranslationStatus
          value={r.i18nJson}
          fields={['title', 'keywords', 'summary', 'seoTitle', 'seoDesc', 'content']}
          onFocus={(code) => openAt(r, code)}
        />
      ),
    },
    {
      key: 'status',
      title: t('plugin_website.product.statusColumn'),
      render: (r) => statusBadge(r.status, t),
    },
    {
      key: 'link',
      title: '',
      align: 'end',
      render: (r) => (r.status === 'online' ? (
        <Button asChild variant="ghost" size="icon" aria-label={t('plugin_website.viewSite')}>
          <a href={`/products/${r.slug}`} target="_blank" rel="noreferrer">
            <ExternalLink aria-hidden="true" />
          </a>
        </Button>
      ) : null),
    },
  ]

  const tabs: { value: string; label: string }[] = [
    { value: ALL, label: t('plugin_website.product.tabAll') },
    { value: 'online', label: t('plugin_website.product.statusOnline') },
    { value: 'offline', label: t('plugin_website.product.statusOffline') },
    { value: 'draft', label: t('plugin_website.product.statusDraft') },
  ]

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_website.product.title')}
        description={t('plugin_website.product.desc')}
        actions={
          <Can perm="website:setting">
            <Button variant="outline" onClick={() => setAliOpen(true)}>
              <Settings2 aria-hidden="true" />
              {t('plugin_website.ali.configEntry')}
              {aliConfig && !(aliConfig.hasSecret && aliConfig.hasSession) ? (
                <Badge variant="warning">{t('plugin_website.ali.configMissing')}</Badge>
              ) : null}
            </Button>
          </Can>
        }
      />
      <FilterBar
        onSearch={() => { setFilters(toContentQuery(draft)); setPage(1); setSelected([]) }}
        onReset={() => { setDraft({}); setFilters({}); setStatusTab(ALL); setPage(1); setSelected([]) }}
      >
        <Input
          className="md:w-56"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_website.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        {categories.length ? (
          <CategoryPicker
            value={draft.categoryIds ?? []}
            onChange={(ids) => setDraft((d) => ({ ...d, categoryIds: ids.length ? ids : undefined }))}
            categories={flatCats}
            className="md:w-44"
          />
        ) : null}
        {extraLocales.length ? (
          <OptionPicker
            title={t('plugin_website.i18n.filterLabel')}
            value={draft.missing ?? []}
            onChange={(v) => setDraft((d) => ({ ...d, missing: v.length ? v : undefined }))}
            options={extraLocales.map((l) => ({
              value: l.code,
              label: t('plugin_website.i18n.filterNoTranslation', { lang: l.label }),
            }))}
            className="md:w-52"
            placeholder={t('plugin_website.i18n.filterLabel')}
          />
        ) : null}
      </FilterBar>
      <Tabs value={statusTab} onValueChange={(v) => { setStatusTab(v); setPage(1); setSelected([]) }}>
        <TabsList>
          {tabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>{tab.label}</TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-website-products"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => { setPage(p); setPageSize(s) },
        }}
        emptyText={t('plugin_website.empty')}
        toolbar={
          <Can perm="website:product">
            {/* 新建落在默认（源）语言：开在英文页签会把 slug/价格/图片等共用字段全部置灰，
                第一眼就是一张点不动的表单；英文母本由上架时的必填校验兜底（会自动跳到英文页签）。 */}
            <Button onClick={() => openAt(null)}>
              <Plus aria-hidden="true" />
              {t('plugin_website.product.create')}
            </Button>
            <Button variant="outline" onClick={() => void onExport()} disabled={exporting}>
              <Download className="me-2 size-4" aria-hidden="true" />
              {t('common.export')}
            </Button>
            {/* 导入在页内弹窗完成（模板下载/上传/失败清单）；台账与更多来源在导入中心。 */}
            <Button variant="outline" onClick={() => setImportOpen(true)}>
              <FileUp className="me-2 size-4" aria-hidden="true" />
              {t('plugin_website.product.importEntry')}
            </Button>
          </Can>
        }
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="website:product">
            <Button variant="outline" size="sm" onClick={() => void applyStatus(ids, 'online')}>
              {t('plugin_website.product.onlineBatch')}
            </Button>
            <Button variant="outline" size="sm" onClick={() => void applyStatus(ids, 'offline')}>
              {t('plugin_website.product.offlineBatch')}
            </Button>
            <Button variant="outline" size="sm" onClick={() => setSyncTarget({ mode: 'batch', ids })}>
              <CloudUpload className="me-2 size-4" aria-hidden="true" />
              {t('plugin_website.ali.syncBatch')}
            </Button>
            <Button variant="destructive" size="sm" onClick={() => void onDelete(selectedRows)}>
              <Trash2 className="me-2 size-4" aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(row) => (
          <Can perm="website:product">
            <Button variant="ghost" size="icon" aria-label={t('plugin_website.post.preview')} onClick={() => void onPreview(row)}>
              <Eye aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('plugin_website.product.copy')} onClick={() => void onCopy(row)}>
              <Copy aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openAt(row)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('plugin_website.ali.syncAction')} onClick={() => setSyncTarget({ mode: 'single', product: row })}>
              <CloudUpload aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete([row])}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {formOpen && (
        <ProductFormDialog
          key={`${editing?.id ?? 'new'}-${focus?.code ?? ''}-${focus?.nonce ?? 0}`}
          onOpenChange={setFormOpen}
          editing={editing}
          initialLocale={focus?.code}
        />
      )}
      {syncTarget && <AliSyncDialog target={syncTarget} onOpenChange={(v) => !v && setSyncTarget(null)} />}
      {importOpen && <ProductImportDialog onOpenChange={setImportOpen} />}
      {aliOpen && <AliConfigDialog onOpenChange={setAliOpen} />}
      <Lightbox
        open={!!preview}
        onOpenChange={(o) => !o && setPreview(null)}
        slides={preview ? [{ src: preview.coverUrl, alt: preview.title }] : []}
      />
    </PageContainer>
  )
}
