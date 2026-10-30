import { useState } from 'react'
import { Plug } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/framework/request/http'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

interface LdapConfig {
  enabled: boolean
  host: string
  port: number
  useTLS: boolean
  startTLS: boolean
  bindDN: string
  baseDN: string
  userFilter: string
  attrUsername: string
  attrEmail: string
  attrNickname: string
  matchExisting: boolean
  autoCreate: boolean
  defaultRole: string
  timeoutMs: number
  caCert: string
  hasBindPassword: boolean
}

export default function LdapPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const config = useQuery({ queryKey: ['ldap', 'config'], queryFn: () => api.get<LdapConfig>('/plugins/ldap/config') })
  const [form, setForm] = useState<Partial<LdapConfig>>({})
  const [bindPassword, setBindPassword] = useState('')

  const cur = { ...config.data, ...form } as LdapConfig
  const set = (k: string, v: unknown) => setForm((f) => ({ ...f, [k]: v }))

  const save = useMutation({
    mutationFn: () => api.put('/plugins/ldap/config', { ...form, bindPassword }),
    onSuccess: () => {
      toast.success(t('common.operateSuccess'))
      setBindPassword('')
      void qc.invalidateQueries({ queryKey: ['ldap'] })
    },
  })
  const test = useMutation({
    mutationFn: () => api.post('/plugins/ldap/test'),
    onSuccess: () => toast.success(t('plugin_ldap.testOk')),
    onError: (e) => toast.error((e as Error).message),
  })

  const val = (k: keyof LdapConfig, fallback = '') => (form[k as keyof typeof form] as string) ?? (config.data?.[k] as string) ?? fallback

  return (
    <PageContainer>
      <PageHeader title={t('plugin_ldap.title')} description={t('plugin_ldap.description')} />
      <Card className="max-w-lg space-y-3 p-6">
        <div className="flex items-center gap-2">
          <Plug className="size-5 text-muted-foreground" aria-hidden="true" />
          <span className="text-sm font-medium">{t('plugin_ldap.enabled')}</span>
          <Switch checked={cur.enabled ?? false} onCheckedChange={(v) => set('enabled', v)} />
        </div>
        <label className="flex flex-col gap-1 text-sm">
          {t('plugin_ldap.host')} <Input value={val('host')} onChange={(e) => set('host', e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          {t('plugin_ldap.port')} <Input type="number" value={val('port', '389')} onChange={(e) => set('port', Number(e.target.value) || 389)} />
        </label>
        <div className="flex gap-4 text-sm">
          <label className="flex items-center gap-1">
            ldaps <Switch checked={cur.useTLS ?? false} onCheckedChange={(v) => set('useTLS', v)} />
          </label>
          <label className="flex items-center gap-1">
            StartTLS <Switch checked={cur.startTLS ?? false} onCheckedChange={(v) => set('startTLS', v as never)} />
          </label>
        </div>
        <label className="flex flex-col gap-1 text-sm">
          Bind DN <Input value={val('bindDN')} onChange={(e) => set('bindDN', e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          Bind Password
          <Input type="password" value={bindPassword} onChange={(e) => setBindPassword(e.target.value)} placeholder={config.data?.hasBindPassword ? t('plugin_ldap.pwKeep') : ''} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          {t('plugin_ldap.caCert')}
          <Textarea
            rows={4}
            className="font-mono text-xs"
            value={val('caCert')}
            onChange={(e) => set('caCert', e.target.value)}
            placeholder={'-----BEGIN CERTIFICATE-----'}
          />
          <span className="text-xs text-muted-foreground">{t('plugin_ldap.caCertHint')}</span>
        </label>
        <label className="flex flex-col gap-1 text-sm">
          Base DN <Input value={val('baseDN')} onChange={(e) => set('baseDN', e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          User Filter <Input value={val('userFilter', '(uid=%s)')} onChange={(e) => set('userFilter', e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          Attr Username <Input value={val('attrUsername', 'uid')} onChange={(e) => set('attrUsername', e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          Attr Email <Input value={val('attrEmail', 'mail')} onChange={(e) => set('attrEmail', e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          Attr Nickname <Input value={val('attrNickname', 'cn')} onChange={(e) => set('attrNickname', e.target.value)} />
        </label>
        <div className="flex gap-4 text-sm">
          <label className="flex items-center gap-1">
            {t('plugin_ldap.matchExisting')} <Switch checked={cur.matchExisting ?? false} onCheckedChange={(v) => set('matchExisting', v)} />
          </label>
          <label className="flex items-center gap-1">
            {t('plugin_ldap.autoCreate')} <Switch checked={cur.autoCreate ?? false} onCheckedChange={(v) => set('autoCreate', v)} />
          </label>
        </div>
        <div className="flex gap-2">
          <Button onClick={() => save.mutate()} disabled={save.isPending}>
            {t('common.save')}
          </Button>
          <Button variant="outline" onClick={() => test.mutate()} disabled={test.isPending}>
            {t('plugin_ldap.test')}
          </Button>
          <Badge variant={cur.enabled ? 'success' : 'secondary'}>{cur.enabled ? t('plugin_ldap.on') : t('plugin_ldap.off')}</Badge>
        </div>
      </Card>
    </PageContainer>
  )
}
