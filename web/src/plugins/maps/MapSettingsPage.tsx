import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { LocationField, type GeoPoint } from '@/framework/map'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Switch } from '@/framework/ui/switch'
import { useMapsConfig, useSaveMapsConfig, type MapProviderId, type MapsConfigInput } from './api'

const EMPTY: MapsConfigInput = {
  amap: { enabled: false, publicKey: '', serverKey: '' },
  baidu: { enabled: false, publicKey: '', serverKey: '' },
  google: { enabled: false, publicKey: '', serverKey: '' },
}

const ORDER: MapProviderId[] = ['amap', 'baidu', 'google']

export default function MapSettingsPage() {
  const { t } = useTranslation()
  const cfg = useMapsConfig()
  const save = useSaveMapsConfig()
  const [form, setForm] = useState<MapsConfigInput>(EMPTY)
  const [point, setPoint] = useState<GeoPoint | null>(null)

  useEffect(() => {
    if (!cfg.data) return
    setForm({
      amap: { enabled: cfg.data.amap.enabled, publicKey: cfg.data.amap.publicKey, serverKey: '' },
      baidu: { enabled: cfg.data.baidu.enabled, publicKey: cfg.data.baidu.publicKey, serverKey: '' },
      google: { enabled: cfg.data.google.enabled, publicKey: cfg.data.google.publicKey, serverKey: '' },
    })
  }, [cfg.data])

  const patch = (id: MapProviderId, p: Partial<MapsConfigInput[MapProviderId]>) =>
    setForm((f) => ({ ...f, [id]: { ...f[id], ...p } }))

  const submit = async () => {
    try {
      await save.mutateAsync(form)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <PageContainer>
      <PageHeader title={t('plugin_maps.title')} description={t('plugin_maps.desc')} />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t('plugin_maps.configTitle')}</CardTitle>
          <CardDescription>{t('plugin_maps.configDesc')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-6">
          {ORDER.map((id) => {
            const pv = cfg.data?.[id]
            return (
              <section key={id} className="flex flex-col gap-3 border-t border-border pt-4 first:border-t-0 first:pt-0">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-foreground">{t(`plugin_maps.providers.${id}`)}</span>
                    {pv?.enabled ? <Badge variant="success">{t('plugin_maps.enabledTag')}</Badge> : null}
                  </div>
                  <Switch checked={form[id].enabled} onCheckedChange={(v) => patch(id, { enabled: v })} aria-label={t(`plugin_maps.providers.${id}`)} />
                </div>
                <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor={`${id}-public`}>{t(`plugin_maps.keys.${id}Public`)}</Label>
                    <Input id={`${id}-public`} value={form[id].publicKey} onChange={(e) => patch(id, { publicKey: e.target.value })} />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor={`${id}-server`}>{t(`plugin_maps.keys.${id}Server`)}</Label>
                    <Input
                      id={`${id}-server`}
                      type="password"
                      autoComplete="new-password"
                      value={form[id].serverKey}
                      placeholder={pv?.hasServerKey ? t('plugin_maps.keySaved') : ''}
                      onChange={(e) => patch(id, { serverKey: e.target.value })}
                    />
                  </div>
                </div>
                <p className="text-xs text-muted-foreground">{t(`plugin_maps.keys.${id}Hint`)}</p>
              </section>
            )
          })}
          <div>
            <Button onClick={() => void submit()} disabled={save.isPending}>
              {t('common.save')}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t('plugin_maps.demoTitle')}</CardTitle>
          <CardDescription>{t('plugin_maps.demoDesc')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <LocationField value={point} onChange={setPoint} label={t('plugin_maps.demoLabel')} height={360} />
          {point ? <pre className="num overflow-x-auto rounded-md bg-muted p-3 text-xs">{JSON.stringify(point, null, 2)}</pre> : null}
        </CardContent>
      </Card>
    </PageContainer>
  )
}
