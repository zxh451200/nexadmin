import { useMemo, useState } from 'react'
import { Languages } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { usePermission } from '@/framework/auth/usePermission'
import { DataTable } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Textarea } from '@/framework/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useEntries, usePublish, useRollback, useSaveEntry, useVersions, type Entry } from './api'
import { useLanguages } from '@/framework/i18n/useLanguages'

export default function I18nEditorPage() {
  const { t } = useTranslation()
  const { languages } = useLanguages()
  const { has } = usePermission()
  const canEdit = has('i18n-editor:edit')
  const canPublish = has('i18n-editor:publish')
  const codes = useMemo(() => languages.map((language) => language.code), [languages])
  const [lang, setLang] = useState('')
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const active = codes.includes(lang) ? lang : (codes[0] ?? '')
  const entries = useEntries(active)
  const versions = useVersions(active)
  const save = useSaveEntry()
  const publish = usePublish()
  const rollback = useRollback()
  const rows = useMemo(() => entries.data?.list ?? [], [entries.data?.list])
  const selected = useMemo(() => rows.find((entry) => entry.key === key), [rows, key])

  const selectEntry = (entry: Entry) => {
    setKey(entry.key)
    setValue(entry.value)
  }

  const changeLanguage = (next: string) => {
    setLang(next)
    setKey('')
    setValue('')
  }

  return (
    <PageContainer>
      <PageHeader title={t('plugin_i18n_editor.title')} description={t('plugin_i18n_editor.description')} />
      <div className="flex flex-wrap items-center gap-2">
        <Select value={active} onValueChange={changeLanguage}>
          <SelectTrigger className="w-40" aria-label={t('plugin_i18n_editor.title')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {codes.map((code) => <SelectItem key={code} value={code}>{code}</SelectItem>)}
          </SelectContent>
        </Select>
        {canPublish ? (
          <Button onClick={() => publish.mutate(active)} disabled={!active || publish.isPending}>
            {t('plugin_i18n_editor.publish')}
          </Button>
        ) : null}
        {canPublish ? (
          <Select onValueChange={(version) => rollback.mutate({ lang: active, version: Number(version) })}>
            <SelectTrigger className="w-48" aria-label={t('plugin_i18n_editor.rollback')}>
              <SelectValue placeholder={t('plugin_i18n_editor.rollback')} />
            </SelectTrigger>
            <SelectContent>
              {(versions.data ?? []).map((item) => (
                <SelectItem key={item.version} value={String(item.version)}>
                  {t('plugin_i18n_editor.version', { version: item.version })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(260px,1fr)_minmax(320px,1fr)]">
        <DataTable<Entry>
          data={rows}
          rowKey="id"
          loading={entries.isLoading}
          isError={entries.isError}
          onRetry={() => void entries.refetch()}
          retrying={entries.isFetching}
          total={entries.data?.total}
          emptyText={t('plugin_i18n_editor.empty')}
          activeRowId={selected?.id}
          onRowClick={canEdit ? selectEntry : undefined}
          columns={[
            {
              key: 'key',
              title: t('plugin_i18n_editor.columns.key'),
              mobile: 'title',
              render: (entry) => <span className="block max-w-[16rem] truncate" title={entry.key}>{entry.key}</span>,
            },
            {
              key: 'value',
              title: t('plugin_i18n_editor.columns.value'),
              render: (entry) => entry.value
                ? <span className="line-clamp-2 max-w-64" title={entry.value}>{entry.value}</span>
                : <span className="text-muted-foreground">-</span>,
            },
            { key: 'status', title: t('plugin_i18n_editor.columns.status') },
          ]}
          rowActions={canEdit ? (entry) => (
            <Button size="sm" variant="outline" className="min-h-11 md:min-h-8" onClick={() => selectEntry(entry)}>
              {t('plugin_i18n_editor.edit')}
            </Button>
          ) : undefined}
        />

        {canEdit ? (
          <section className="h-fit space-y-3 rounded-lg border bg-muted/20 p-4" aria-label={t('plugin_i18n_editor.edit')}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-sm font-medium text-foreground">{t('plugin_i18n_editor.edit')}</h2>
              <div className="flex items-center gap-1.5">
                <Badge variant="secondary" className="gap-1">
                  <Languages className="size-3.5" aria-hidden="true" />
                  {active}
                </Badge>
                {selected ? (
                  <span className="text-xs text-muted-foreground">
                    {t('plugin_i18n_editor.currentVersion', { version: selected.version })}
                  </span>
                ) : null}
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="i18n-entry-key">{t('plugin_i18n_editor.key')}</Label>
              <Input id="i18n-entry-key" value={key} onChange={(event) => setKey(event.target.value)} placeholder={t('plugin_i18n_editor.key')} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="i18n-entry-value">{t('plugin_i18n_editor.value')}</Label>
              <Textarea id="i18n-entry-value" value={value} onChange={(event) => setValue(event.target.value)} placeholder={t('plugin_i18n_editor.value')} />
            </div>
            <Button disabled={!key.trim() || !active || save.isPending} onClick={() => save.mutate({ lang: active, key: key.trim(), value })}>
              {t('plugin_i18n_editor.save')}
            </Button>
          </section>
        ) : null}
      </div>
    </PageContainer>
  )
}
