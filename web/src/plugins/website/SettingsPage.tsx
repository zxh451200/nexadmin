import { useCallback, useEffect, useRef, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
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
  useSettings,
  type FriendLink,
  type WebsiteLimits,
  type SiteSettingsForm,
  type SocialLink,
  type SocialPlatform,
} from './api'

/** 不随语言变化的字段：留在页面本地 state，译文态下置灰。 */
type SharedTextKey =
  | 'siteName' | 'logoUrl' | 'faviconUrl' | 'ogImageUrl' | 'headCode' | 'titleSuffix' | 'homeSlug' | 'homeCtaHref' | 'contactEmail'
  | 'contactPhone' | 'qrCodeUrl' | 'qrCodeCaption' | 'icp' | 'contactConsentUrl' | 'theme' | 'defaultLang'
type SharedSettings = Pick<SiteSettingsForm, SharedTextKey | 'marketEnabled' | 'messageNotifyEmail' | 'contactConsent'> & {
  socials: SocialLink[]
  friendLinks: FriendLink[]
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

/**
 * 分区导航：按「谁在什么时候改它」切组，形态与平台「系统设置」一致
 * （左栏分区 + ?tab= 深链，只渲染当前分区）。整份表单仍是一次保存。
 */
const TABS = ['brand', 'home', 'seo', 'footer', 'features'] as const
type TabKey = (typeof TABS)[number]

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

  // 提示文案与「添加」按钮的上限都按后端下发的生效值渲染，同一组数值不再前后各抄一份。
  const mediaHint = { types: limits.media.labels.join(' / '), max: `${limits.media.maxMb}MB` }
  const socialMaxed = shared.socials.length >= limits.socialMax
  const friendMaxed = shared.friendLinks.length >= limits.friendMax

  const set = (key: SharedTextKey, v: string) => onSharedChange({ ...shared, [key]: v })

  // 当前选中主题（'' = default）：决定是否显示「导入演示内容」按钮。
  const selectedTheme = themesData?.items.find((x) => x.id === (shared.theme || 'default'))
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

  // 邮箱格式校验：设置侧此前没有（投稿侧有）；留空合法——联系邮箱选填
  const emailInvalid = shared.contactEmail.trim() !== '' && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(shared.contactEmail.trim())
  // 未保存提示：shared 相对「载入/上次保存」的快照有变化即脏；刷新与关窗由浏览器原生拦截。
  // 多语言字段在 Provider 内部，脏标记只覆盖共用字段——大头都在这边。
  const savedRef = useRef<SharedSettings | null>(null)
  const [dirty, setDirty] = useState(false)
  useEffect(() => {
    if (shared && !savedRef.current) savedRef.current = shared
  }, [shared])
  useEffect(() => {
    if (shared && savedRef.current) setDirty(JSON.stringify(shared) !== JSON.stringify(savedRef.current))
  }, [shared])
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
      setDirty(false)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
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
                <Select
                  value={shared.theme || '__default'}
                  disabled={sharedDisabled}
                  onValueChange={(v) => set('theme', v === '__default' ? '' : v)}
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
                  previewClassName="size-9"
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
                  previewClassName="size-9"
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
                  previewClassName="h-24 w-40 object-cover"
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
                  onChange={(e) => set('headCode', e.target.value)}
                  placeholder={t('plugin_website.placeholder.headCode', { defaultValue: '' })}
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
            </>
          ) : null}
          {tab === 'footer' ? (
            <>
              {sharedInput('contactEmail', emailInvalid ? t('plugin_website.settings.emailInvalid') : undefined)}
              {sharedInput('contactPhone')}
              <SharedFieldShell labelKey="plugin_website.fields.qrCodeUrl" hint={t('plugin_website.hint.qrCodeUrl', mediaHint)}>
                <ImageUrlField
                  value={shared.qrCodeUrl}
                  onChange={(v) => set('qrCodeUrl', v)}
                  title={t('plugin_website.fields.qrCodeUrl')}
                  crop="free"
                  disabled={sharedDisabled}
                  placeholder={t('plugin_website.placeholder.qrCodeUrl', { defaultValue: '' })}
                  previewClassName="size-9 bg-background"
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
            <>
              <SwitchRow
                label={t('plugin_website.message.notifyEmail')}
                description={t('plugin_website.message.notifyEmailHint')}
                checked={shared.messageNotifyEmail}
                disabled={sharedDisabled}
                onCheckedChange={(v) => onSharedChange({ ...shared, messageNotifyEmail: v })}
              />
              <SwitchRow
                label={t('plugin_website.message.consent')}
                description={t('plugin_website.message.consentHint')}
                checked={shared.contactConsent}
                disabled={sharedDisabled}
                onCheckedChange={(v) => onSharedChange({ ...shared, contactConsent: v })}
              />
              {/* 政策地址只在开启后出现：关着时摆一个必填框只会让人以为现在就在拦投稿 */}
              {shared.contactConsent ? sharedInput('contactConsentUrl') : null}
              <SwitchRow
                label={t('plugin_website.market.enabled')}
                description={t('plugin_website.market.enabledHint')}
                checked={shared.marketEnabled}
                disabled={sharedDisabled}
                onCheckedChange={(v) => onSharedChange({ ...shared, marketEnabled: v })}
              />
            </>
          ) : null}
        </section>
        <LocaleFormActions />
        {/* 保存条常驻：五个分区共用一份表单，切分区不丢改动，也不该让人以为每个分区各存一次 */}
        <div className="flex items-center gap-3">
          {dirty ? <span className="text-xs text-warning">{t('plugin_website.settings.unsaved')}</span> : null}
          <Button onClick={() => void submit()} disabled={save.isPending || emailInvalid}>{t('common.save')}</Button>
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
        homeSlug: data.homeSlug ?? '',
        homeCtaHref: data.homeCtaHref,
        theme: data.theme ?? '',
        defaultLang: data.defaultLang ?? '',
        contactEmail: data.contactEmail,
        contactPhone: data.contactPhone,
        qrCodeUrl: data.qrCodeUrl,
        qrCodeCaption: data.qrCodeCaption,
        icp: data.icp,
        marketEnabled: data.marketEnabled,
        messageNotifyEmail: data.messageNotifyEmail,
        contactConsent: data.contactConsent,
        contactConsentUrl: data.contactConsentUrl ?? '',
        socials: data.socials ?? [],
        friendLinks: data.friendLinks ?? [],
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
      <PageHeader title={t('plugin_website.settings.title')} description={t('plugin_website.settings.desc')} />
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
