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
import { Skeleton } from '@/framework/ui/skeleton'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import {
  translateContent,
  uploadMedia,
  useI18nSchema,
  useLanguages,
  useSaveSettings,
  useSettings,
  type FriendLink,
  type SiteSettingsForm,
  type SocialLink,
  type SocialPlatform,
} from './api'

/** 不随语言变化的字段：留在页面本地 state，译文态下置灰。 */
type SharedTextKey =
  | 'siteName' | 'logoUrl' | 'homeCtaHref' | 'contactEmail'
  | 'contactPhone' | 'qrCodeUrl' | 'qrCodeCaption' | 'icp'
type SharedSettings = Pick<SiteSettingsForm, SharedTextKey | 'marketEnabled'> & {
  socials: SocialLink[]
  friendLinks: FriendLink[]
}

/** 社交平台下拉的顺序与文案；值集合与后端 socialPlatforms 一致。 */
const SOCIAL_PLATFORMS: SocialPlatform[] = ['github', 'x', 'linkedin', 'youtube', 'wechat', 'weibo', 'bilibili', 'rss', 'mail']

/** 可翻译字段：归 LocaleFormProvider，与后端 setting schema 同源。 */
type LocalizedKey = 'siteDesc' | 'homeTitle' | 'homeSubtitle' | 'homeCtaLabel' | 'seoKeywords' | 'footerNote'

/**
 * 表单主体：整份表单只有一套，切语言换的是数据源。
 * 可翻译字段交给 LocaleFormProvider，站点名/logo/联系方式等共用字段留在本地 state。
 */
function SettingsFormBody({
  shared, onSharedChange,
}: {
  shared: SharedSettings
  onSharedChange: (next: SharedSettings) => void
}) {
  const { t } = useTranslation()
  const save = useSaveSettings()
  const lf = useLocaleForm()
  const { disabled: sharedDisabled } = useSharedField()
  const [uploading, setUploading] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const [uploadingQr, setUploadingQr] = useState(false)
  const qrRef = useRef<HTMLInputElement>(null)

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

  const submit = async () => {
    const base = lf.baseValues()
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
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const sharedInput = (key: SharedTextKey) => (
    <SharedFieldShell key={key} labelKey={`plugin_website.fields.${key}`}>
      <Input
        value={shared[key]}
        disabled={sharedDisabled}
        onChange={(e) => set(key, e.target.value)}
        placeholder={t(`plugin_website.placeholder.${key}`, { defaultValue: '' })}
      />
    </SharedFieldShell>
  )

  const localizedField = (key: LocalizedKey) => (
    <LocalizedField key={key} fieldKey={key} placeholder={t(`plugin_website.placeholder.${key}`, { defaultValue: '' })} />
  )

  return (
    <>
      <div className="flex items-center justify-between gap-2 border-b pb-4">
        <LocaleSwitcher />
        <span className="text-xs text-muted-foreground">{t('plugin_website.i18n.switchHint')}</span>
      </div>
      <SwitchRow
        label={t('plugin_website.market.enabled')}
        description={t('plugin_website.market.enabledHint')}
        checked={shared.marketEnabled}
        disabled={sharedDisabled}
        onCheckedChange={(v) => onSharedChange({ ...shared, marketEnabled: v })}
      />
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
          {shared.logoUrl && (
            <img
              src={shared.logoUrl}
              alt=""
              className="h-9 w-9 shrink-0 rounded-md border object-cover"
              onError={(e) => { e.currentTarget.style.visibility = 'hidden' }}
            />
          )}
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
        <span className="text-xs text-muted-foreground">{t('plugin_website.hint.logoUrl')}</span>
      </SharedFieldShell>
      <div className="flex flex-col gap-1">
        {localizedField('homeTitle')}
        <span className="text-xs text-muted-foreground">{t('plugin_website.hint.homeTitle')}</span>
      </div>
      {localizedField('homeSubtitle')}
      {localizedField('homeCtaLabel')}
      {sharedInput('homeCtaHref')}
      {localizedField('seoKeywords')}
      {sharedInput('contactEmail')}
      {sharedInput('contactPhone')}
      <SharedFieldShell labelKey="plugin_website.fields.qrCodeUrl">
        <div className="flex items-center gap-2">
          <Input
            value={shared.qrCodeUrl}
            disabled={sharedDisabled}
            onChange={(e) => set('qrCodeUrl', e.target.value)}
            placeholder={t('plugin_website.placeholder.qrCodeUrl', { defaultValue: '' })}
          />
          {shared.qrCodeUrl && (
            <img
              src={shared.qrCodeUrl}
              alt=""
              className="size-9 shrink-0 rounded-md border bg-background object-contain"
              onError={(e) => { e.currentTarget.style.visibility = 'hidden' }}
            />
          )}
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
      {/* 文件选择器不放进任何 label 里，避免点标签文字误开文件弹窗 */}
      <input
        ref={fileRef}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif"
        className="hidden"
        onChange={(e) => void onPickLogo(e.target.files?.[0])}
      />
      <input
        ref={qrRef}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif"
        className="hidden"
        onChange={(e) => void onPickQr(e.target.files?.[0])}
      />
      {/* 语言清单已归平台管理（系统设置 → 语言管理），这里只给入口提示，不再复制一份编辑器 */}
      <p className="text-xs text-muted-foreground">{t('plugin_website.languages.manageHint')}</p>
      <LocaleFormActions />
      <div>
        <Button onClick={() => void submit()} disabled={save.isPending}>{t('common.save')}</Button>
      </div>
    </>
  )
}

export default function SettingsPage() {
  const { t } = useTranslation()
  const { data, isLoading } = useSettings()
  const { data: languages = [] } = useLanguages()
  const { data: schema } = useI18nSchema('setting')
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
        homeCtaHref: data.homeCtaHref,
        contactEmail: data.contactEmail,
        contactPhone: data.contactPhone,
        qrCodeUrl: data.qrCodeUrl,
        qrCodeCaption: data.qrCodeCaption,
        icp: data.icp,
        marketEnabled: data.marketEnabled,
        socials: data.socials ?? [],
        friendLinks: data.friendLinks ?? [],
      })
    }
  }, [data, shared])

  // schema、语言清单与设置都是表单的前提，没到货前只出骨架屏——否则会闪出一个没有字段的空表单。
  if (isLoading || !data || !shared || !schema || !defaultLocale) {
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
      <div className="grid w-full max-w-2xl gap-4 rounded-lg border bg-card p-5">
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
          <SettingsFormBody shared={shared} onSharedChange={setShared} />
        </LocaleFormProvider>
      </div>
    </PageContainer>
  )
}
