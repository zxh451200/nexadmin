import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { CloudUpload, Copy, ExternalLink, Eye, Pencil, Plus, Trash2, Upload, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Can } from '@/framework/auth/Can'
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
import { ImageUrlField } from '@/framework/components/ImageUrlField'
import { Uploader, type UploadedFile } from '@/framework/components/Uploader'
import { VideoUrlField } from '@/framework/components/VideoUrlField'
import { slugify } from './format'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { EditorSchemaGate } from './EditorSchemaGate'
import TranslationStatus from './TranslationStatus'
import {
  fetchAliCategories,
  fetchAliTest,
  fetchProductPreview,
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
  useSaveAliConfig,
  useSaveProduct,
  useSetProductStatus,
  useSyncProduct,
  useSyncProductBatch,
  type AliCategoryNode,
  type AliMarket,
  type ContentQuery,
  type ProductCategoryNode,
  type ProductSpec,
  type ProductStatus,
  type SiteProduct,
} from './api'

const ALL = '__all'

/** 图集上限，与后端 maxImagesPerProduct 同值：界面先挡住，别等保存才报错。 */
const IMAGES_MAX = 10

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

/** 图集：一组 ImageUrlField + 删除，上限与后端 maxImagesPerProduct 对齐；「批量上传」走 Uploader 一次选多张。 */
function ImagesField({
  images, onChange, disabled,
}: {
  images: string[]
  onChange: (images: string[]) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [batchOpen, setBatchOpen] = useState(false)
  const [batchFiles, setBatchFiles] = useState<UploadedFile[]>([])
  const update = (index: number, value: string) => {
    onChange(images.map((img, i) => (i === index ? value : img)))
  }
  const remove = (index: number) => {
    onChange(images.filter((_, i) => i !== index))
  }
  const atLimit = images.length >= IMAGES_MAX
  const openBatch = () => {
    setBatchFiles([])
    setBatchOpen(true)
  }
  // 关弹窗才算提交：此时已上传完成的 URL 追加进图集（上限在打开时就用 maxCount 锁死，不会超）。
  const closeBatch = (open: boolean) => {
    if (!open) {
      const urls = batchFiles.map((f) => f.url).filter(Boolean)
      if (urls.length) onChange([...images, ...urls])
    }
    setBatchOpen(open)
  }
  return (
    <div className="flex flex-col gap-2">
      {images.map((img, i) => (
        <div key={i} className="flex items-start gap-2">
          <ImageUrlField
            value={img}
            onChange={(v) => update(i, v)}
            disabled={disabled}
            title={t('plugin_website.product.imageUpload')}
            previewClassName="h-16 w-16 object-cover p-0"
            aria-label={`${t('plugin_website.product.images')} ${i + 1}`}
          />
          <Button
            variant="ghost"
            size="icon"
            className="text-destructive"
            disabled={disabled}
            aria-label={t('common.delete')}
            onClick={() => remove(i)}
          >
            <X aria-hidden="true" />
          </Button>
        </div>
      ))}
      {atLimit ? (
        <span className="text-xs text-muted-foreground">{t('plugin_website.product.imageLimit', { max: IMAGES_MAX })}</span>
      ) : (
        <div className="flex items-center gap-3">
          <Button variant="link" size="sm" className="h-auto w-fit p-0" disabled={disabled} onClick={() => onChange([...images, ''])}>
            <Plus aria-hidden="true" />
            {t('plugin_website.product.imageAdd')}
          </Button>
          <Button variant="link" size="sm" className="h-auto w-fit p-0" disabled={disabled} onClick={openBatch}>
            <Upload aria-hidden="true" />
            {t('plugin_website.product.imageBatch')}
          </Button>
        </div>
      )}
      <ResponsiveDialog
        open={batchOpen}
        onOpenChange={closeBatch}
        size="md"
        title={t('plugin_website.product.imageBatch')}
        description={t('plugin_website.product.imageLimit', { max: IMAGES_MAX })}
      >
        <Uploader
          kind="image"
          visibility="public"
          multiple
          maxCount={IMAGES_MAX - images.length}
          value={batchFiles}
          onChange={setBatchFiles}
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
  const [priceUnit, setPriceUnit] = useState(editing?.priceUnit ?? '')
  const [moq, setMoq] = useState(editing?.moq ? String(editing.moq) : '')
  const [leadTimeDays, setLeadTimeDays] = useState(editing?.leadTimeDays ? String(editing.leadTimeDays) : '')
  const [port, setPort] = useState(editing?.port ?? '')
  const [paymentTerms, setPaymentTerms] = useState(editing?.paymentTerms ?? '')
  const [specs, setSpecs] = useState<ProductSpec[]>(() => safeParseSpecs(editing?.specsJson))
  const [seoKeywords, setSeoKeywords] = useState(editing?.seoKeywords ?? '')
  const [noindex, setNoindex] = useState(editing?.noindex ?? false)
  const [author, setAuthor] = useState(editing?.author ?? '')
  const [catIds, setCatIds] = useState<string[]>(() => (editing?.categories ?? []).map((c) => c.id))
  const flatCats = useMemo(() => flattenCatTree(buildCatTree(categories)), [categories])
  // 类目多了以后找类目全靠滚：按名称过滤，命中节点连同祖先链一起保留，缩进层级不丢。
  const [catQuery, setCatQuery] = useState('')
  const visibleCats = useMemo(() => {
    const q = catQuery.trim().toLowerCase()
    if (!q) return flatCats
    const byId = new Map(flatCats.map((c) => [c.id, c]))
    const keep = new Set<string>()
    for (const c of flatCats) {
      if (c.title.toLowerCase().includes(q)) {
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
    return flatCats.filter((c) => keep.has(c.id))
  }, [flatCats, catQuery])
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
      <div className="flex flex-col gap-3">
        <p className="text-sm font-medium">{t('plugin_website.product.basicSection')}</p>
        <LocalizedField fieldKey="title" required />
        <LocalizedField fieldKey="keywords" aiTarget="keywords" placeholder={t('plugin_website.fields.productKeywordsPlaceholder')} />
        <LocalizedField fieldKey="summary" />
        <SharedFieldShell labelKey="plugin_website.fields.slug">
          <Input
            maxLength={64}
            value={slug}
            disabled={sharedDisabled}
            onChange={(e) => setSlug(e.target.value)}
            placeholder={t('plugin_website.fields.slugAuto')}
            className="font-mono"
          />
          <span className="text-xs text-muted-foreground">{productPath}</span>
        </SharedFieldShell>
        <SharedGroup labelKey="plugin_website.product.categories">
          <div className="flex flex-col rounded-md border border-border p-1" role="group" aria-label={t('plugin_website.product.categories')}>
            {flatCats.length ? (
              <Input
                value={catQuery}
                onChange={(e) => setCatQuery(e.target.value)}
                disabled={sharedDisabled}
                placeholder={t('plugin_website.product.categorySearch')}
                aria-label={t('plugin_website.product.categorySearch')}
                className="m-1 h-8"
              />
            ) : null}
            <div className="flex max-h-56 flex-col overflow-y-auto">
              {visibleCats.length ? visibleCats.map((c) => (
                <label
                  key={c.id}
                  className="flex min-h-9 cursor-pointer items-center gap-2 rounded-sm text-sm hover:bg-accent/60"
                  style={{ paddingInlineStart: `${(c.depth - 1) * 16 + 8}px` }}
                >
                  <Checkbox
                    checked={catIds.includes(c.id)}
                    disabled={sharedDisabled}
                    onCheckedChange={(on) => setCatIds((ids) => (on ? [...ids, c.id] : ids.filter((x) => x !== c.id)))}
                    aria-label={c.title}
                  />
                  <span className="min-w-0 flex-1 truncate">{c.title}</span>
                  <span className="num text-xs text-muted-foreground">{c.productCount}</span>
                </label>
              )) : (
                <p className="px-2 py-3 text-xs text-muted-foreground">
                  {t(flatCats.length ? 'plugin_website.product.categoryNoMatch' : 'plugin_website.product.categoriesEmpty')}
                </p>
              )}
            </div>
          </div>
        </SharedGroup>
        <p className="text-sm font-medium">{t('plugin_website.product.imageSection')}</p>
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
        <SharedGroup labelKey="plugin_website.product.images">
          <ImagesField images={images} onChange={setImages} disabled={sharedDisabled} />
        </SharedGroup>
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
        <p className="text-sm font-medium">{t('plugin_website.product.tradeSection')}</p>
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
            <Input
              value={currency}
              maxLength={8}
              disabled={sharedDisabled}
              onChange={(e) => setCurrency(e.target.value.toUpperCase())}
              placeholder="USD"
            />
          </SharedFieldShell>
          <SharedFieldShell labelKey="plugin_website.product.priceUnit">
            <Input
              value={priceUnit}
              maxLength={32}
              disabled={sharedDisabled}
              onChange={(e) => setPriceUnit(e.target.value)}
              placeholder="piece"
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
        <SharedFieldShell labelKey="plugin_website.product.port">
          <Input
            value={port}
            maxLength={64}
            disabled={sharedDisabled}
            onChange={(e) => setPort(e.target.value)}
            placeholder={t('plugin_website.product.portPlaceholder')}
          />
        </SharedFieldShell>
        <SharedFieldShell labelKey="plugin_website.product.paymentTerms">
          <Input
            value={paymentTerms}
            maxLength={100}
            disabled={sharedDisabled}
            onChange={(e) => setPaymentTerms(e.target.value)}
            placeholder="T/T, L/C"
          />
        </SharedFieldShell>
        <SharedGroup labelKey="plugin_website.product.specs">
          <SpecsEditor specs={specs} onChange={setSpecs} disabled={sharedDisabled} />
        </SharedGroup>
        <LocalizedField fieldKey="content" className="min-h-40" />
        <LocalizedField fieldKey="seoTitle" />
        <LocalizedField fieldKey="seoDesc" />
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
    </ResponsiveDialog>
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
 * 阿里开放平台凭据卡：AppKey 明文，Secret/SessionKey 密码框（已配置时占位提示「留空保持不变」）。
 * 保存走独立 PUT（凭据三态在后端处理），连接测试调 category 接口验证网关+签名。
 */
function AliConfigCard() {
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
    <div className="rounded-lg border border-border p-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <p className="font-medium">{t('plugin_website.ali.configTitle')}</p>
        <span className="text-xs text-muted-foreground">
          {config?.hasSecret ? t('plugin_website.ali.secretConfigured') : t('plugin_website.ali.secretMissing')}
          {' · '}
          {config?.hasSession ? t('plugin_website.ali.sessionConfigured') : t('plugin_website.ali.sessionMissing')}
        </span>
      </div>
      <div className="mt-2 grid grid-cols-1 gap-2 md:grid-cols-3">
        <Input
          value={appKey}
          onChange={(e) => setAppKey(e.target.value)}
          placeholder={t('plugin_website.ali.appKey')}
          aria-label={t('plugin_website.ali.appKey')}
          // 这张卡形似登录表单（多个输入 + 密码框），浏览器会把站内账号密码自动填进来：
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
      <div className="mt-2 flex flex-wrap items-center gap-2">
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
        <Button size="sm" onClick={() => void submit()} disabled={save.isPending}>
          {t('plugin_website.ali.save')}
        </Button>
        {testState === 'ok' ? <Badge variant="success">{t('plugin_website.ali.testOk')}</Badge> : null}
        {testState === 'fail' ? <Badge variant="destructive">{t('plugin_website.ali.testFail')}</Badge> : null}
      </div>
    </div>
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
  const { t } = useTranslation()
  const confirm = useConfirm()
  // 预取字段 schema：点「新建/编辑」时直接命中缓存，不再先闪一个加载弹窗。
  useI18nSchema('product')
  const { data: languages = [] } = useLanguages()
  const { data: categories = [] } = useProductCategories()
  const [draft, setDraft] = useState<ContentQuery>({})
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

  const selectedRows = (data?.list ?? []).filter((r) => selected.includes(r.id))

  const columns: Column<SiteProduct>[] = [
    {
      key: 'title',
      title: t('plugin_website.product.titleColumn'),
      mobile: 'title',
      render: (r) => (
        <span className="flex min-w-0 items-center gap-2">
          {r.coverUrl ? (
            <img src={r.coverUrl} alt="" className="h-10 w-10 flex-none rounded-md border border-border object-cover" />
          ) : (
            <span className="flex h-10 w-10 flex-none items-center justify-center rounded-md border border-border text-sm text-muted-foreground">
              {r.title.slice(0, 1).toUpperCase()}
            </span>
          )}
          <span className="min-w-0">
            <span className="block truncate font-medium text-foreground">{r.title}</span>
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
      <PageHeader title={t('plugin_website.product.title')} description={t('plugin_website.product.desc')} />
      <Can perm="website:setting">
        <AliConfigCard />
      </Can>
      <FilterBar
        onSearch={() => { setFilters(draft); setPage(1); setSelected([]) }}
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
          <Select
            value={draft.categoryId ?? ALL}
            onValueChange={(v) => setDraft((d) => ({ ...d, categoryId: v === ALL ? undefined : v }))}
          >
            <SelectTrigger className="md:w-44" aria-label={t('plugin_website.product.categoryColumn')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('plugin_website.product.categoryAll')}</SelectItem>
              {categories.map((c) => (
                <SelectItem key={c.id} value={c.id}>{c.title}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
        {extraLocales.length ? (
          <Select
            value={draft.missing ?? ALL}
            onValueChange={(v) => setDraft((d) => ({ ...d, missing: v === ALL ? undefined : v }))}
          >
            <SelectTrigger className="md:w-48" aria-label={t('plugin_website.i18n.filterLabel')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('plugin_website.filter.allLanguages')}</SelectItem>
              {extraLocales.map((l) => (
                <SelectItem key={l.code} value={l.code}>
                  {t('plugin_website.i18n.filterNoTranslation', { lang: l.label })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
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
    </PageContainer>
  )
}
