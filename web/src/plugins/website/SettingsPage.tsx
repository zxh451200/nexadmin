import { useCallback, useEffect, useRef, useState } from 'react'
import { Upload } from 'lucide-react'
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
import { Skeleton } from '@/framework/ui/skeleton'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import {
  translateContent,
  uploadMedia,
  useI18nSchema,
  useLanguages,
  useSaveSettings,
  useSettings,
  type SiteSettingsForm,
} from './api'

/** 不随语言变化的字段：留在页面本地 state，译文态下置灰。 */
type SharedTextKey = 'siteName' | 'logoUrl' | 'homeCtaHref' | 'contactEmail' | 'icp'
type SharedSettings = Pick<SiteSettingsForm, SharedTextKey | 'marketEnabled'>

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
      <div className="flex items-start justify-between gap-4 border-b pb-4">
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium">{t('plugin_website.market.enabled')}</span>
          <span className="text-xs text-muted-foreground">{t('plugin_website.market.enabledHint')}</span>
        </div>
        <Switch
          checked={shared.marketEnabled}
          disabled={sharedDisabled}
          onCheckedChange={(v) => onSharedChange({ ...shared, marketEnabled: v })}
          aria-label={t('plugin_website.market.enabled')}
        />
      </div>
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
      {sharedInput('icp')}
      {localizedField('footerNote')}
      {/* 文件选择器不放进任何 label 里，避免点标签文字误开文件弹窗 */}
      <input
        ref={fileRef}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif"
        className="hidden"
        onChange={(e) => void onPickLogo(e.target.files?.[0])}
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
        icp: data.icp,
        marketEnabled: data.marketEnabled,
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
