import { useCallback, useEffect, useRef, useState } from 'react'
import { Plus, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { SwitchRow } from '@/framework/components/SwitchField'
import { ZoomableImage } from '@/framework/components/ZoomableImage'
import { Skeleton } from '@/framework/ui/skeleton'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import {
  translateContent,
  uploadMedia,
  useI18nSchema,
  useLanguages,
  useMediaLimits,
  useSaveSettings,
  useSettings,
  type FriendLink,
  type MediaLimits,
  type SiteSettingsForm,
  type SocialLink,
  type SocialPlatform,
} from './api'

/** 不随语言变化的字段：留在页面本地 state，译文态下置灰。 */
type SharedTextKey =
  | 'siteName' | 'logoUrl' | 'faviconUrl' | 'ogImageUrl' | 'headCode' | 'titleSuffix' | 'homeCtaHref' | 'contactEmail'
  | 'contactPhone' | 'qrCodeUrl' | 'qrCodeCaption' | 'icp' | 'contactConsentUrl'
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
 * 表单主体：整份表单只有一套，切语言换的是数据源。
 * 可翻译字段交给 LocaleFormProvider，站点名/logo/联系方式等共用字段留在本地 state。
 */
function SettingsFormBody({
  shared, onSharedChange, mediaLimits,
}: {
  shared: SharedSettings
  onSharedChange: (next: SharedSettings) => void
  mediaLimits: MediaLimits
}) {
  const { t } = useTranslation()
  const save = useSaveSettings()
  const lf = useLocaleForm()
  const { disabled: sharedDisabled } = useSharedField()
  const [uploading, setUploading] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const [uploadingFav, setUploadingFav] = useState(false)
  const favRef = useRef<HTMLInputElement>(null)
  const [uploadingQr, setUploadingQr] = useState(false)
  const qrRef = useRef<HTMLInputElement>(null)
  // 分享图与封面同一套规则：https:// 或 / 开头，别的拦住——og:image 跟着指向它
  const ogInvalid = shared.ogImageUrl.trim() !== '' && !/^(https?:\/\/|\/)/i.test(shared.ogImageUrl.trim())

  // 文件选择器与提示文案都按后端下发的生效限制渲染，同一组数值不再前后各抄一份。
  const acceptImages = mediaLimits.mimes.join(',')
  const mediaHint = { types: mediaLimits.labels.join(' / '), max: `${mediaLimits.maxMb}MB` }

  const set = (key: SharedTextKey, v: string) => onSharedChange({ ...shared, [key]: v })

  const onPickLogo = async (file: File | undefined) => {
    if (!file) return
    setUploading(true)
    try {
      const res = await uploadMedia(file)
      set('logoUrl', res.url)
      toast.success(t('plugin_website.logo.uploaded'))
    } catch {
      toast.error(t('common.operateFailed'))
    } finally {
      setUploading(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  const onPickQr = async (file: File | undefined) => {
    if (!file) return
    setUploadingQr(true)
    try {
      const res = await uploadMedia(file)
      set('qrCodeUrl', res.url)
      toast.success(t('plugin_website.qr.uploaded'))
    } catch {
      toast.error(t('common.operateFailed'))
    } finally {
      setUploadingQr(false)
      if (qrRef.current) qrRef.current.value = ''
    }
  }

  const onPickFav = async (file: File | undefined) => {
    if (!file) return
    setUploadingFav(true)
    try {
      const res = await uploadMedia(file)
      set('faviconUrl', res.url)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    } finally {
      setUploadingFav(false)
      if (favRef.current) favRef.current.value = ''
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

  const sharedInput = (key: SharedTextKey, error?: string) => (
    <SharedFieldShell key={key} labelKey={`plugin_website.fields.${key}`}>
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

  const localizedField = (key: LocalizedKey) => (
    <LocalizedField key={key} fieldKey={key} placeholder={t(`plugin_website.placeholder.${key}`, { defaultValue: '' })} />
  )

  return (
    <>
      <div className="flex items-center justify-between gap-2 px-1">
        <LocaleSwitcher />
        <span className="text-xs text-muted-foreground">{t('plugin_website.i18n.switchHint')}</span>
      </div>
      {/* 25+ 个控件按「谁在什么时候改它」分成六节，别再一列到底 */}
      <section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
        <h3 className="text-sm font-medium">{t('plugin_website.settings.section.identity')}</h3>
        {sharedInput('siteName')}
        {localizedField('siteDesc')}
      <SharedFieldShell labelKey="plugin_website.fields.logoUrl">
        <div className="flex items-center gap-2">
          <Input
            value={shared.logoUrl}
            disabled={sharedDisabled}
            onChange={(e) => set('logoUrl', e.target.value)}
            placeholder={t('plugin_website.placeholder.logoUrl', { defaultValue: '' })}
          />
          {shared.logoUrl && <ZoomableImage src={shared.logoUrl} className="h-9 w-9 p-0.5" />}
          <Button
            type="button"
            variant="outline"
            disabled={uploading || sharedDisabled}
            onClick={() => fileRef.current?.click()}
          >
            <Upload aria-hidden="true" />
            {uploading ? t('plugin_website.logo.uploading') : t('plugin_website.logo.upload')}
          </Button>
        </div>
        <span className="text-xs text-muted-foreground">{t('plugin_website.hint.logoUrl', mediaHint)}</span>
      </SharedFieldShell>
      <SharedFieldShell labelKey="plugin_website.fields.faviconUrl">
        <div className="flex items-center gap-2">
          <Input
            value={shared.faviconUrl}
            disabled={sharedDisabled}
            onChange={(e) => set('faviconUrl', e.target.value)}
            placeholder={t('plugin_website.placeholder.faviconUrl', { defaultValue: '' })}
          />
          {shared.faviconUrl && <ZoomableImage src={shared.faviconUrl} className="h-9 w-9 p-0.5" />}
          <Button type="button" variant="outline" disabled={uploadingFav || sharedDisabled} onClick={() => favRef.current?.click()}>
            <Upload aria-hidden="true" />
            {uploadingFav ? t('plugin_website.logo.uploading') : t('plugin_website.logo.upload')}
          </Button>
        </div>
        <span className="text-xs text-muted-foreground">{t('plugin_website.hint.faviconUrl', mediaHint)}</span>
      </SharedFieldShell>
      </section>
      <section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
        <h3 className="text-sm font-medium">{t('plugin_website.settings.section.home')}</h3>
        <div className="flex flex-col gap-1">
        {localizedField('homeTitle')}
        <span className="text-xs text-muted-foreground">{t('plugin_website.hint.homeTitle')}</span>
      </div>
      {localizedField('homeSubtitle')}
      {localizedField('homeCtaLabel')}
      {sharedInput('homeCtaHref')}
      </section>
      <section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
        <h3 className="text-sm font-medium">{t('plugin_website.settings.section.seo')}</h3>
        {localizedField('seoKeywords')}
      {sharedInput('titleSuffix')}
      <SharedFieldShell labelKey="plugin_website.fields.ogImageUrl">
        <Input
          value={shared.ogImageUrl}
          disabled={sharedDisabled}
          aria-invalid={ogInvalid}
          onChange={(e) => set('ogImageUrl', e.target.value)}
          placeholder={t('plugin_website.placeholder.ogImageUrl', { defaultValue: '' })}
        />
        {shared.ogImageUrl ? <ZoomableImage src={shared.ogImageUrl} className="h-24 w-40 object-cover" /> : null}
        {ogInvalid ? <span className="text-xs text-destructive">{t('plugin_website.fields.coverInvalid')}</span> : null}
      </SharedFieldShell>
      <SharedFieldShell labelKey="plugin_website.fields.headCode">
        <Textarea
          rows={4}
          value={shared.headCode}
          disabled={sharedDisabled}
          maxLength={5000}
          onChange={(e) => set('headCode', e.target.value)}
          placeholder={t('plugin_website.placeholder.headCode', { defaultValue: '' })}
        />
        <span className="text-xs text-muted-foreground">{t('plugin_website.hint.headCode')}</span>
      </SharedFieldShell>
      </section>
      <section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
        <h3 className="text-sm font-medium">{t('plugin_website.settings.section.contact')}</h3>
      {sharedInput('contactEmail', emailInvalid ? t('plugin_website.settings.emailInvalid') : undefined)}
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
      {sharedInput('contactPhone')}
      </section>
      <section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
        <h3 className="text-sm font-medium">{t('plugin_website.settings.section.footer')}</h3>
      <SharedFieldShell labelKey="plugin_website.fields.qrCodeUrl">
        <div className="flex items-center gap-2">
          <Input
            value={shared.qrCodeUrl}
            disabled={sharedDisabled}
            onChange={(e) => set('qrCodeUrl', e.target.value)}
            placeholder={t('plugin_website.placeholder.qrCodeUrl', { defaultValue: '' })}
          />
          {shared.qrCodeUrl && <ZoomableImage src={shared.qrCodeUrl} className="size-9 bg-background" />}
          <Button type="button" variant="outline" disabled={uploadingQr || sharedDisabled} onClick={() => qrRef.current?.click()}>
            <Upload aria-hidden="true" />
            {uploadingQr ? t('plugin_website.logo.uploading') : t('plugin_website.qr.upload')}
          </Button>
        </div>
        <span className="text-xs text-muted-foreground">{t('plugin_website.hint.qrCodeUrl')}</span>
      </SharedFieldShell>
      {sharedInput('qrCodeCaption')}
      {sharedInput('icp')}
      {localizedField('footerNote')}
      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium">{t('plugin_website.settings.socials')}</span>
        <span className="text-xs text-muted-foreground">{t('plugin_website.hint.socials')}</span>
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
        <Button type="button" variant="outline" size="sm" className="w-fit" disabled={sharedDisabled} onClick={addSocial}>
          <Plus className="size-4" aria-hidden="true" />
          {t('plugin_website.settings.addSocial')}
        </Button>
      </div>
      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium">{t('plugin_website.settings.friendLinks')}</span>
        <span className="text-xs text-muted-foreground">{t('plugin_website.hint.friendLinks')}</span>
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
        <Button type="button" variant="outline" size="sm" className="w-fit" disabled={sharedDisabled} onClick={addFriend}>
          <Plus className="size-4" aria-hidden="true" />
          {t('plugin_website.settings.addFriendLink')}
        </Button>
      </div>
      </section>
      <section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
        <h3 className="text-sm font-medium">{t('plugin_website.settings.section.market')}</h3>
        <SwitchRow
          label={t('plugin_website.market.enabled')}
          description={t('plugin_website.market.enabledHint')}
          checked={shared.marketEnabled}
          disabled={sharedDisabled}
          onCheckedChange={(v) => onSharedChange({ ...shared, marketEnabled: v })}
        />
      </section>
      {/* 文件选择器不放进任何 label 里，避免点标签文字误开文件弹窗 */}
      <input
        ref={fileRef}
        type="file"
        accept={acceptImages}
        className="hidden"
        onChange={(e) => void onPickLogo(e.target.files?.[0])}
      />
      <input
        ref={favRef}
        type="file"
        accept={acceptImages}
        className="hidden"
        onChange={(e) => void onPickFav(e.target.files?.[0])}
      />
      <input
        ref={qrRef}
        type="file"
        accept={acceptImages}
        className="hidden"
        onChange={(e) => void onPickQr(e.target.files?.[0])}
      />
      {/* 语言清单已归平台管理（系统设置 → 语言管理），这里只给入口提示，不再复制一份编辑器 */}
      <p className="text-xs text-muted-foreground">{t('plugin_website.languages.manageHint')}</p>
      <LocaleFormActions />
      <div className="flex items-center gap-3">
        {dirty ? <span className="text-xs text-warning">{t('plugin_website.settings.unsaved')}</span> : null}
        <Button onClick={() => void submit()} disabled={save.isPending || emailInvalid}>{t('common.save')}</Button>
      </div>
    </>
  )
}

export default function SettingsPage() {
  const { t } = useTranslation()
  const { data, isLoading } = useSettings()
  const { data: languages = [] } = useLanguages()
  const { data: schema } = useI18nSchema('setting')
  const { data: mediaLimits } = useMediaLimits()
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
        homeCtaHref: data.homeCtaHref,
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

  // schema、语言清单、设置与图片上传限制都是表单的前提，没到货前只出骨架屏——否则会闪出一个没有字段的空表单。
  if (isLoading || !data || !shared || !schema || !defaultLocale || !mediaLimits) {
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
      <div className="flex w-full max-w-2xl flex-col gap-4">
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
          <SettingsFormBody shared={shared} onSharedChange={setShared} mediaLimits={mediaLimits} />
        </LocaleFormProvider>
      </div>
    </PageContainer>
  )
}
