import { useState } from 'react'
import { Plug } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/framework/request/http'
import { PlatformNotice } from '@/framework/components/PlatformNotice'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { SwitchRow } from '@/framework/components/SwitchField'
import { usePermission } from '@/framework/auth/usePermission'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
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

/** LDAP/AD 配置：作为「系统设置 → 安全与登录」的一个分区渲染，旧地址 /ldap/config 重定向到 ?tab=ldap。 */
export function LdapSection() {
  const { t } = useTranslation()
  // 这一页写的都是没有 tenant_id 的全局行：非平台调用方只读（后端 core.RequirePlatform() 挡）；
  // 保存/测试走 ldap:edit，没授权就把按钮收掉（后端二次把关，别让人填完整表单才吃 403）。
  const { isPlatform, has } = usePermission()
  const canEdit = has('ldap:edit')
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
    <Card className="max-w-lg space-y-3 p-6">
      <PlatformNotice />
      <SwitchRow
        label={
          <span className="inline-flex items-center gap-2">
            <Plug className="size-4 text-muted-foreground" aria-hidden="true" />
            {t('plugin_ldap.enabled')}
          </span>
        }
        checked={cur.enabled ?? false}
        onCheckedChange={(v) => set('enabled', v)}
      />
      <label className="flex flex-col gap-1 text-sm">
        <span>
          {t('plugin_ldap.host')}
          <RequiredMark />
        </span>
        <Input value={val('host')} onChange={(e) => set('host', e.target.value)} />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        {t('plugin_ldap.port')} <Input type="number" value={val('port', '389')} onChange={(e) => set('port', Number(e.target.value) || 389)} />
      </label>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <SwitchRow label={t('plugin_ldap.ldaps')} checked={cur.useTLS ?? false} onCheckedChange={(v) => set('useTLS', v)} bare />
        <SwitchRow label={t('plugin_ldap.startTLS')} checked={cur.startTLS ?? false} onCheckedChange={(v) => set('startTLS', v)} bare />
      </div>
      <label className="flex flex-col gap-1 text-sm">
        <span>
          {t('plugin_ldap.bindDn')}
          <RequiredMark />
        </span>
        <Input value={val('bindDN')} onChange={(e) => set('bindDN', e.target.value)} />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        {t('plugin_ldap.bindPassword')}
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
        <span>
          {t('plugin_ldap.baseDn')}
          <RequiredMark />
        </span>
        <Input value={val('baseDN')} onChange={(e) => set('baseDN', e.target.value)} />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        {t('plugin_ldap.userFilter')} <Input value={val('userFilter', '(uid=%s)')} onChange={(e) => set('userFilter', e.target.value)} />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        {t('plugin_ldap.attrUsername')} <Input value={val('attrUsername', 'uid')} onChange={(e) => set('attrUsername', e.target.value)} />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        {t('plugin_ldap.attrEmail')} <Input value={val('attrEmail', 'mail')} onChange={(e) => set('attrEmail', e.target.value)} />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        {t('plugin_ldap.attrNickname')} <Input value={val('attrNickname', 'cn')} onChange={(e) => set('attrNickname', e.target.value)} />
      </label>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <SwitchRow label={t('plugin_ldap.matchExisting')} checked={cur.matchExisting ?? false} onCheckedChange={(v) => set('matchExisting', v)} bare />
        <SwitchRow label={t('plugin_ldap.autoCreate')} checked={cur.autoCreate ?? false} onCheckedChange={(v) => set('autoCreate', v)} bare />
      </div>
      <div className="flex gap-2">
        <Button onClick={() => save.mutate()} disabled={!isPlatform || !canEdit || save.isPending}>
          {t('common.save')}
        </Button>
        <Button variant="outline" onClick={() => test.mutate()} disabled={!isPlatform || !canEdit || test.isPending}>
          {t('plugin_ldap.test')}
        </Button>
        <Badge variant={cur.enabled ? 'success' : 'secondary'}>{cur.enabled ? t('plugin_ldap.on') : t('plugin_ldap.off')}</Badge>
      </div>
    </Card>
  )
}
