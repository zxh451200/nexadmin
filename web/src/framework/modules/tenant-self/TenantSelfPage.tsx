import { useEffect, useMemo, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { Building2, HardDrive, RotateCcw, Save, ShieldCheck, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ChartCard } from '@/framework/components/ChartCard'
import { EmptyState } from '@/framework/components/EmptyState'
import { ImageUrlField } from '@/framework/components/ImageUrlField'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { StatCard } from '@/framework/components/StatCard'
import { TrendChart } from '@/framework/components/TrendChart'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Skeleton } from '@/framework/ui/skeleton'
import { useSaveTenantSelf, useTenantSelf, type TenantSelf, type TenantSelfInput } from './api'

const EMPTY: TenantSelfInput = { companyName: '', contactName: '', contactPhone: '', contactEmail: '', logoUrl: '' }
const EMAIL_RE = /^[^@\s]+@[^@\s]+$/

const toForm = (d: TenantSelf): TenantSelfInput => ({
  companyName: d.companyName,
  contactName: d.contactName,
  contactPhone: d.contactPhone,
  contactEmail: d.contactEmail,
  logoUrl: d.logoUrl,
})

const formatMb = (mb: number) => (mb >= 1024 ? `${+(mb / 1024).toFixed(1)} GB` : `${mb} MB`)
const percent = (used: number, max: number) => (max > 0 ? Math.min(100, Math.round((used / max) * 100)) : 0)
const tone = (p: number) => (p >= 100 ? 'destructive' : p >= 80 ? 'warning' : 'primary')

function InfoRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 py-2">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="truncate text-sm font-medium text-foreground">{children}</dd>
    </div>
  )
}

function Field({
  id,
  label,
  value,
  onChange,
  error,
  type = 'text',
  autoComplete,
}: {
  id: string
  label: string
  value: string
  onChange: (v: string) => void
  error?: string
  type?: string
  autoComplete?: string
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} type={type} value={value} autoComplete={autoComplete} onChange={(e) => onChange(e.target.value)} aria-invalid={!!error} />
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
    </div>
  )
}

export default function TenantSelfPage() {
  const { t, i18n } = useTranslation()
  const { data, isLoading, isError } = useTenantSelf()
  const save = useSaveTenantSelf()
  const [form, setForm] = useState<TenantSelfInput>(EMPTY)
  const [errors, setErrors] = useState<Partial<Record<keyof TenantSelfInput, string>>>({})

  useEffect(() => {
    if (data) setForm(toForm(data))
  }, [data])

  const trend = useMemo(
    () => (data?.trend ?? []).map((p) => ({ day: p.day.slice(5), users: p.users, roles: p.roles })),
    [data?.trend],
  )

  const header = (
    <PageHeader
      title={t('tenantSelf.title')}
      description={t('tenantSelf.description')}
      actions={
        data ? (
          <div className="flex items-center gap-2">
            <Badge variant={data.status === 'active' ? 'success' : data.status === 'expired' ? 'destructive' : 'warning'}>
              {t(`tenantSelf.status.${data.status}`)}
            </Badge>
            <span className="text-xs text-muted-foreground">
              {new Date(data.expireAt).toLocaleDateString(i18n.language)} ·{' '}
              {data.daysLeft >= 0 ? t('tenantSelf.daysLeft', { count: data.daysLeft }) : t('tenantSelf.expiredDays', { count: -data.daysLeft })}
            </span>
          </div>
        ) : null
      }
    />
  )

  if (isLoading) {
    return (
      <PageContainer>
        {header}
        <div className="grid gap-3 md:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-28 rounded-lg" />
          ))}
        </div>
        <Skeleton className="h-72 rounded-lg" />
      </PageContainer>
    )
  }

  if (isError || !data) {
    return (
      <PageContainer>
        {header}
        <Card>
          <CardContent>
            <EmptyState icon={<Building2 className="size-6" aria-hidden="true" />} title={t('tenantSelf.unmanaged')} description={t('tenantSelf.unmanagedDesc')} />
          </CardContent>
        </Card>
      </PageContainer>
    )
  }

  const u = data.usage
  const usersPct = percent(u.users, u.maxUsers)
  const storagePct = percent(u.storageMB, u.maxStorageMB)
  const rolesPct = percent(u.roles, u.maxRoles)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const next: typeof errors = {}
    if (form.contactEmail && !EMAIL_RE.test(form.contactEmail.trim())) next.contactEmail = t('tenantSelf.errors.email')
    if (form.logoUrl && !/^https?:\/\/[^\s]+$/i.test(form.logoUrl.trim()) && !/^\/(?!\/)/.test(form.logoUrl.trim())) {
      next.logoUrl = t('tenantSelf.errors.logo')
    }
    setErrors(next)
    if (Object.keys(next).length > 0) return
    save.mutate(
      { ...form, contactEmail: form.contactEmail.trim(), logoUrl: form.logoUrl.trim() },
      {
        onSuccess: () => toast.success(t('tenantSelf.profile.saved')),
        onError: () => toast.error(t('tenantSelf.profile.saveFailed')),
      },
    )
  }

  const set = (k: keyof TenantSelfInput) => (v: string) => setForm((f) => ({ ...f, [k]: v }))

  return (
    <PageContainer>
      {header}

      <div className="grid gap-3 md:grid-cols-3">
        <StatCard
          title={t('tenantSelf.usage.users')}
          value={u.users}
          hint={u.maxUsers > 0 ? `/ ${u.maxUsers}` : t('tenantSelf.unlimited')}
          progress={u.maxUsers > 0 ? usersPct : undefined}
          tone={tone(usersPct)}
          icon={<Users aria-hidden="true" />}
        />
        <StatCard
          title={t('tenantSelf.usage.storage')}
          value={formatMb(u.storageMB)}
          hint={u.maxStorageMB > 0 ? `/ ${formatMb(u.maxStorageMB)}` : t('tenantSelf.unlimited')}
          progress={u.maxStorageMB > 0 ? storagePct : undefined}
          tone={tone(storagePct)}
          icon={<HardDrive aria-hidden="true" />}
        />
        <StatCard
          title={t('tenantSelf.usage.roles')}
          value={u.roles}
          hint={u.maxRoles > 0 ? `/ ${u.maxRoles}` : t('tenantSelf.unlimited')}
          progress={u.maxRoles > 0 ? rolesPct : undefined}
          tone={tone(rolesPct)}
          icon={<ShieldCheck aria-hidden="true" />}
        />
      </div>

      <div className="grid gap-3 lg:grid-cols-5">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="text-base font-medium">{t('tenantSelf.info.title')}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-1 gap-x-6 divide-y divide-border sm:grid-cols-2 sm:divide-y-0">
              <InfoRow label={t('tenantSelf.info.name')}>{data.name}</InfoRow>
              <InfoRow label={t('tenantSelf.info.domain')}>{data.domain}</InfoRow>
              <InfoRow label={t('tenantSelf.info.pkg')}>{data.packageName || '—'}</InfoRow>
              <InfoRow label={t('tenantSelf.info.expire')}>{new Date(data.expireAt).toLocaleDateString(i18n.language)}</InfoRow>
              <InfoRow label={t('tenantSelf.info.ai')}>
                {data.aiEnabled
                  ? data.aiDailyQuota > 0
                    ? t('tenantSelf.aiPerDay', { count: data.aiDailyQuota })
                    : t('tenantSelf.unlimited')
                  : t('tenantSelf.disabled')}
              </InfoRow>
              <InfoRow label={t('tenantSelf.info.status')}>{t(`tenantSelf.status.${data.status}`)}</InfoRow>
            </dl>
          </CardContent>
        </Card>

        <ChartCard className="lg:col-span-3" title={t('tenantSelf.trend.title')}>
          {trend.length < 2 ? (
            <EmptyState title={t('tenantSelf.trend.empty')} description={t('tenantSelf.trend.emptyDesc')} />
          ) : (
            <TrendChart
              data={trend}
              xKey="day"
              type="area"
              height={230}
              series={[
                { key: 'users', label: t('tenantSelf.usage.users') },
                { key: 'roles', label: t('tenantSelf.usage.roles') },
              ]}
            />
          )}
        </ChartCard>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">{t('tenantSelf.profile.title')}</CardTitle>
          <p className="text-xs text-muted-foreground">{t('tenantSelf.profile.desc')}</p>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} noValidate className="space-y-5">
            <div className="grid gap-4 md:grid-cols-3">
              <Field id="tenant-company" label={t('tenantSelf.profile.companyName')} value={form.companyName} onChange={set('companyName')} />
              <Field id="tenant-contact" label={t('tenantSelf.profile.contactName')} value={form.contactName} onChange={set('contactName')} autoComplete="name" />
              <Field id="tenant-phone" label={t('tenantSelf.profile.contactPhone')} value={form.contactPhone} onChange={set('contactPhone')} type="tel" autoComplete="tel" />
              <Field
                id="tenant-email"
                label={t('tenantSelf.profile.contactEmail')}
                value={form.contactEmail}
                onChange={set('contactEmail')}
                error={errors.contactEmail}
                type="email"
                autoComplete="email"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="tenant-logo">{t('tenantSelf.profile.logo')}</Label>
              <ImageUrlField
                value={form.logoUrl}
                onChange={set('logoUrl')}
                title={t('tenantSelf.profile.logo')}
                hint={t('tenantSelf.profile.logoHint')}
                placeholder="/uploads/logo.png"
                aria-label={t('tenantSelf.profile.logo')}
              />
              {errors.logoUrl ? <p className="text-xs text-destructive">{errors.logoUrl}</p> : null}
            </div>
            <div className="flex flex-wrap justify-end gap-2 border-t border-border pt-4">
              <Button type="button" variant="ghost" disabled={save.isPending} onClick={() => { setForm(toForm(data)); setErrors({}) }}>
                <RotateCcw aria-hidden="true" />
                {t('tenantSelf.profile.reset')}
              </Button>
              <Button type="submit" disabled={save.isPending}>
                <Save aria-hidden="true" />
                {t('tenantSelf.profile.save')}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </PageContainer>
  )
}
