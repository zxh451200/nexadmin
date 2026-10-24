import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Skeleton } from '@/framework/ui/skeleton'
import { useAbout, useLicenses, type LicenseRow } from './api'

const ALL = '__all__'
// 统计标签的固定顺序；其余类型按出现顺序跟在后面
const MAIN_TYPES = ['MIT', 'Apache-2.0', 'BSD', 'ISC']

// 把 BSD-2-Clause / BSD-3-Clause 归为 BSD 一类
const typeOf = (license: string) => (license.startsWith('BSD') ? 'BSD' : license)

export default function AboutPage() {
  const { t } = useTranslation()
  const about = useAbout()
  const lic = useLicenses()
  const [keyword, setKeyword] = useState('')
  const [type, setType] = useState(ALL)

  const rows = useMemo(() => lic.data?.list ?? [], [lic.data])

  const stats = useMemo(() => {
    const m = new Map<string, number>(MAIN_TYPES.map((k) => [k, 0]))
    for (const r of rows) m.set(typeOf(r.license), (m.get(typeOf(r.license)) ?? 0) + 1)
    return Array.from(m.entries()).filter(([k, n]) => n > 0 || MAIN_TYPES.includes(k))
  }, [rows])

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase()
    return rows.filter((r) => (type === ALL || typeOf(r.license) === type) && (!kw || r.name.toLowerCase().includes(kw)))
  }, [rows, keyword, type])

  const info = about.data
  const items: { label: string; value: string }[] = info
    ? [
        { label: t('about.version'), value: info.version },
        { label: t('about.buildTime'), value: new Date(info.buildTime).toLocaleString() },
        { label: t('about.dbEngine'), value: info.dbEngine },
        { label: t('about.goVersion'), value: info.goVersion },
        { label: t('about.os'), value: info.os },
      ]
    : []

  const columns: Column<LicenseRow>[] = [
    { key: 'name', title: t('about.name'), mobile: 'title' },
    { key: 'version', title: t('about.pkgVersion'), render: (r) => <span className="num">{r.version}</span> },
    { key: 'license', title: t('about.license'), render: (r) => <Badge variant="outline">{r.license}</Badge> },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('about.title')} description={t('about.description')} />

      <Card>
        <CardContent className="pt-6">
          {about.isLoading ? (
            <Skeleton className="h-20 w-full" />
          ) : (
            <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5">
              {items.map((it) => (
                <div key={it.label} className="min-w-0">
                  <dt className="text-xs text-muted-foreground">{it.label}</dt>
                  <dd className="num mt-1 break-words text-sm font-medium text-foreground">{it.value}</dd>
                </div>
              ))}
            </dl>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('about.licensesTitle')}</CardTitle>
          <CardDescription>{t('about.licensesHint')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-wrap gap-2">
            {stats.map(([k, n]) => (
              <Badge key={k} variant="secondary">
                {k} · {n}
              </Badge>
            ))}
          </div>
          <div className="flex flex-col gap-2 md:flex-row">
            <Input
              className="md:w-64"
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder={t('about.searchPlaceholder')}
              aria-label={t('common.keyword')}
            />
            <Select value={type} onValueChange={setType}>
              <SelectTrigger className="md:w-48" aria-label={t('about.license')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{t('about.allLicenses')}</SelectItem>
                {stats.map(([k]) => (
                  <SelectItem key={k} value={k}>
                    {k}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <DataTable<LicenseRow> columns={columns} data={filtered} rowKey="name" loading={lic.isLoading} pagination={false} />
        </CardContent>
      </Card>
    </PageContainer>
  )
}
