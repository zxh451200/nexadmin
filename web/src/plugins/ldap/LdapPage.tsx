import { useState } from 'react'
import { Plug } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/framework/request/http'
import { PlatformNotice } from '@/framework/components/PlatformNotice'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { SwitchRow } from '@/framework/components/SwitchField'
import { FormGrid, FormGridFull } from '@/framework/components/FormLayout'
import { usePermission } from '@/framework/auth/usePermission'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/framework/ui/card'
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

/** 目录服务器类型模板：同一套字段在不同目录下口径差异最大的就是这几项（Keycloak/Gitea 的
    LDAP 配置同样按 vendor 给默认值）。点按钮填入表单，仍可手改；模板本身不落库。 */
const LDAP_PRESETS = [
  { label: 'OpenLDAP', values: { userFilter: '(uid=%s)', attrUsername: 'uid', attrNickname: 'cn', attrEmail: 'mail' } },
  { label: 'Active Directory', values: { userFilter: '(sAMAccountName=%s)', attrUsername: 'sAMAccountName', attrNickname: 'displayName', attrEmail: 'mail' } },
]

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
    // 测的是当前表单填写值（含未保存的修改）：绑定密码留空沿用已保存密码，后端不落库。
    // 不带 body 的话后端只测已保存配置，刚改完就点测试会测到旧值，最迷惑人。
    mutationFn: () => api.post('/plugins/ldap/test', { ...cur, bindPassword }),
    onSuccess: () => toast.success(t('plugin_ldap.testOk')),
    // 错误 toast 由 http 拦截器统一弹出，这里再 toast 一次会变双份。
  })

  const val = (k: keyof LdapConfig, fallback = '') => (form[k as keyof typeof form] as string) ?? (config.data?.[k] as string) ?? fallback

  const field = 'flex flex-col gap-1 text-sm'
  const hint = 'text-xs text-muted-foreground'

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('plugin_ldap.title')}</CardTitle>
        <CardDescription>{t('plugin_ldap.description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
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
        <FormGrid cols={2}>
          <label className={field}>
            <span>
              {t('plugin_ldap.host')}
              <RequiredMark />
            </span>
            <Input value={val('host')} onChange={(e) => set('host', e.target.value)} />
          </label>
          <label className={field}>
            {t('plugin_ldap.port')} <Input type="number" value={val('port', '389')} onChange={(e) => set('port', Number(e.target.value) || 389)} />
          </label>
          <label className={field}>
            {t('plugin_ldap.timeoutMs')} <Input type="number" value={val('timeoutMs', '5000')} onChange={(e) => set('timeoutMs', Number(e.target.value) || 5000)} />
          </label>
          <label className={field}>
            {t('plugin_ldap.defaultRole')} <Input value={val('defaultRole')} onChange={(e) => set('defaultRole', e.target.value)} />
            <span className={hint}>{t('plugin_ldap.defaultRoleHint')}</span>
          </label>
        </FormGrid>
        <div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {/* LDAPS 与 StartTLS 都是「整条连接加密」，同时开没有意义（后端 LDAPS 优先），前端直接互斥 */}
            <SwitchRow label={t('plugin_ldap.ldaps')} checked={cur.useTLS ?? false} onCheckedChange={(v) => { set('useTLS', v); if (v) set('startTLS', false) }} bare />
            <SwitchRow label={t('plugin_ldap.startTLS')} checked={cur.startTLS ?? false} onCheckedChange={(v) => { set('startTLS', v); if (v) set('useTLS', false) }} bare />
          </div>
          <span className={`mt-1 block ${hint}`}>{t('plugin_ldap.tlsHint')}</span>
        </div>
        <FormGrid cols={2}>
          <label className={field}>
            {t('plugin_ldap.bindDn')}
            <Input value={val('bindDN')} onChange={(e) => set('bindDN', e.target.value)} />
            <span className={hint}>{t('plugin_ldap.bindDnHint')}</span>
          </label>
          <label className={field}>
            {t('plugin_ldap.bindPassword')}
            <Input type="password" value={bindPassword} onChange={(e) => setBindPassword(e.target.value)} placeholder={config.data?.hasBindPassword ? t('plugin_ldap.pwKeep') : ''} />
          </label>
        </FormGrid>
        <FormGridFull>
          <label className={field}>
            {t('plugin_ldap.caCert')}
            <Textarea
              rows={4}
              className="font-mono text-xs"
              value={val('caCert')}
              onChange={(e) => set('caCert', e.target.value)}
              placeholder={'-----BEGIN CERTIFICATE-----'}
            />
            <span className={hint}>{t('plugin_ldap.caCertHint')}</span>
          </label>
        </FormGridFull>
        <FormGrid cols={2}>
          <FormGridFull>
            <div className={field}>
              <span>{t('plugin_ldap.serverType')}</span>
              <div className="flex flex-wrap gap-2">
                {LDAP_PRESETS.map((p) => (
                  <Button
                    key={p.label}
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => Object.entries(p.values).forEach(([k, v]) => set(k, v))}
                  >
                    {p.label}
                  </Button>
                ))}
              </div>
              <span className={hint}>{t('plugin_ldap.serverTypeHint')}</span>
            </div>
          </FormGridFull>
          <label className={field}>
            <span>
              {t('plugin_ldap.baseDn')}
              <RequiredMark />
            </span>
            <Input value={val('baseDN')} onChange={(e) => set('baseDN', e.target.value)} />
          </label>
          <label className={field}>
            {t('plugin_ldap.userFilter')} <Input value={val('userFilter', '(uid=%s)')} onChange={(e) => set('userFilter', e.target.value)} />
            <span className={hint}>{t('plugin_ldap.filterHint')}</span>
          </label>
          <label className={field}>
            {t('plugin_ldap.attrUsername')} <Input value={val('attrUsername', 'uid')} onChange={(e) => set('attrUsername', e.target.value)} />
            <span className={hint}>{t('plugin_ldap.attrUsernameHint')}</span>
          </label>
          <label className={field}>
            {t('plugin_ldap.attrEmail')} <Input value={val('attrEmail', 'mail')} onChange={(e) => set('attrEmail', e.target.value)} />
          </label>
          <label className={field}>
            {t('plugin_ldap.attrNickname')} <Input value={val('attrNickname', 'cn')} onChange={(e) => set('attrNickname', e.target.value)} />
          </label>
        </FormGrid>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <SwitchRow label={t('plugin_ldap.matchExisting')} checked={cur.matchExisting ?? false} onCheckedChange={(v) => set('matchExisting', v)} bare />
          <SwitchRow label={t('plugin_ldap.autoCreate')} checked={cur.autoCreate ?? false} onCheckedChange={(v) => set('autoCreate', v)} bare />
        </div>
      </CardContent>
      {/* 操作条：左侧状态与测试说明一组，右侧动作；内容区不再夹零散说明行 */}
      <CardFooter className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 flex-col gap-1 sm:flex-row sm:items-center sm:gap-3">
          <Badge variant={cur.enabled ? 'success' : 'secondary'}>{cur.enabled ? t('plugin_ldap.on') : t('plugin_ldap.off')}</Badge>
          <span className={`min-w-0 ${hint}`}>{t('plugin_ldap.testHint')}</span>
        </div>
        <div className="flex shrink-0 gap-2">
          <Button onClick={() => save.mutate()} disabled={!isPlatform || !canEdit || save.isPending}>
            {t('common.save')}
          </Button>
          <Button variant="outline" onClick={() => test.mutate()} disabled={!isPlatform || !canEdit || test.isPending}>
            {t('plugin_ldap.test')}
          </Button>
        </div>
      </CardFooter>
    </Card>
  )
}
