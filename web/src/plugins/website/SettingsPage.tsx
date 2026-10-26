import { useEffect, useRef, useState } from 'react'
import { Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Skeleton } from '@/framework/ui/skeleton'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import TranslationsEditor from './TranslationsEditor'
import { uploadMedia, useSaveSettings, useSettings, type SiteSettingsForm } from './api'

type TextFieldKey = Exclude<keyof SiteSettingsForm, 'marketEnabled'>

const FIELDS: { key: TextFieldKey; area?: boolean }[] = [
  { key: 'siteName' },
  { key: 'siteDesc', area: true },
  { key: 'logoUrl' },
  { key: 'homeTitle', area: true },
  { key: 'homeSubtitle', area: true },
  { key: 'homeCtaLabel' },
  { key: 'homeCtaHref' },
  { key: 'seoKeywords', area: true },
  { key: 'contactEmail' },
  { key: 'icp' },
  { key: 'footerNote', area: true },
]

export default function SettingsPage() {
  const { t } = useTranslation()
  const { data, isLoading } = useSettings()
  const save = useSaveSettings()
  const [form, setForm] = useState<SiteSettingsForm | null>(null)
  const [uploading, setUploading] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (data && !form) {
      setForm({
        siteName: data.siteName,
        siteDesc: data.siteDesc,
        logoUrl: data.logoUrl,
        homeTitle: data.homeTitle,
        homeSubtitle: data.homeSubtitle,
        homeCtaLabel: data.homeCtaLabel,
        homeCtaHref: data.homeCtaHref,
        seoKeywords: data.seoKeywords,
        contactEmail: data.contactEmail,
        icp: data.icp,
        footerNote: data.footerNote,
        marketEnabled: data.marketEnabled,
        i18nJson: data.i18nJson ?? '',
      })
    }
  }, [data, form])

  if (isLoading || !form) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-64 w-full max-w-2xl" />
      </div>
    )
  }

  const set = (key: TextFieldKey, v: string) => setForm({ ...form, [key]: v })

  const onPickLogo = async (file: File | undefined) => {
    if (!file || !form) return
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
    try {
      await save.mutateAsync(form)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.settings.title')} description={t('plugin_website.settings.desc')} />
      <div className="grid w-full max-w-2xl gap-4 rounded-lg border bg-card p-5">
        <div className="flex items-start justify-between gap-4 border-b pb-4">
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium">{t('plugin_website.market.enabled')}</span>
            <span className="text-xs text-muted-foreground">{t('plugin_website.market.enabledHint')}</span>
          </div>
          <Switch
            checked={form.marketEnabled}
            onCheckedChange={(v) => setForm({ ...form, marketEnabled: v })}
            aria-label={t('plugin_website.market.enabled')}
          />
        </div>
        {FIELDS.map(({ key, area }) => (
          <div key={key} className="flex flex-col gap-1.5 text-sm">
            {key === 'logoUrl' ? (
              <>
                <span>{t(`plugin_website.fields.${key}`)}</span>
                <div className="flex items-center gap-2">
                  <Input
                    value={form[key]}
                    onChange={(e) => set(key, e.target.value)}
                    placeholder={t(`plugin_website.placeholder.${key}`, { defaultValue: '' })}
                  />
                  {form.logoUrl && (
                    <img
                      src={form.logoUrl}
                      alt=""
                      className="h-9 w-9 shrink-0 rounded-md border object-cover"
                      onError={(e) => { e.currentTarget.style.visibility = 'hidden' }}
                    />
                  )}
                  <Button
                    variant="outline"
                    disabled={uploading}
                    onClick={() => fileRef.current?.click()}
                  >
                    <Upload aria-hidden="true" />
                    {uploading ? t('plugin_website.logo.uploading') : t('plugin_website.logo.upload')}
                  </Button>
                  <input
                    ref={fileRef}
                    type="file"
                    accept="image/png,image/jpeg,image/webp,image/gif"
                    className="hidden"
                    onChange={(e) => void onPickLogo(e.target.files?.[0])}
                  />
                </div>
                <span className="text-xs text-muted-foreground">{t('plugin_website.hint.logoUrl')}</span>
              </>
            ) : (
              <label className="flex flex-col gap-1.5 text-sm">
                {t(`plugin_website.fields.${key}`)}
                {area ? (
                  <Textarea
                    rows={2}
                    value={form[key]}
                    onChange={(e) => set(key, e.target.value)}
                    placeholder={t(`plugin_website.placeholder.${key}`, { defaultValue: '' })}
                  />
                ) : (
                  <Input
                    value={form[key]}
                    onChange={(e) => set(key, e.target.value)}
                    placeholder={t(`plugin_website.placeholder.${key}`, { defaultValue: '' })}
                  />
                )}
                {key === 'homeTitle' && (
                  <span className="text-xs text-muted-foreground">{t('plugin_website.hint.homeTitle')}</span>
                )}
              </label>
            )}
          </div>
        ))}
        <TranslationsEditor
          value={form.i18nJson}
          onChange={(v) => setForm({ ...form, i18nJson: v })}
          fields={[
            { key: 'siteDesc', label: t('plugin_website.fields.siteDesc'), multiline: true },
            { key: 'homeTitle', label: t('plugin_website.fields.homeTitle'), multiline: true },
            { key: 'homeSubtitle', label: t('plugin_website.fields.homeSubtitle'), multiline: true },
            { key: 'homeCtaLabel', label: t('plugin_website.fields.homeCtaLabel') },
            { key: 'seoKeywords', label: t('plugin_website.fields.seoKeywords'), multiline: true },
            { key: 'footerNote', label: t('plugin_website.fields.footerNote'), multiline: true },
          ]}
        />
        <div>
          <Button onClick={() => void submit()} disabled={save.isPending}>{t('common.save')}</Button>
        </div>
      </div>
    </PageContainer>
  )
}
