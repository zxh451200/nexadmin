import { useEffect, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Skeleton } from '@/framework/ui/skeleton'
import { useAdminLanguages, useSaveLanguages, type LanguageRow } from './api'

const emptyRow = (): LanguageRow => ({
  code: '', name: '', dir: 'ltr', default: false, htmlLang: '', ogLocale: '', prefix: '', dateFormat: '',
})

/**
 * 平台语言清单：全站唯一的语言真源。
 *
 * 这里是整表替换（与后端一致），所以编辑发生在本地副本上，点保存才落库。
 * 前缀或语言集合变了需要重启才对公开站生效——后端会算，界面照实说，不让人白重启。
 */
export function LanguageSection() {
  const { t } = useTranslation()
  const { data, isLoading } = useAdminLanguages()
  const save = useSaveLanguages()
  const [rows, setRows] = useState<LanguageRow[]>([])

  useEffect(() => {
    if (data) setRows(data.map((r) => ({ ...r })))
  }, [data])

  const patch = (i: number, next: Partial<LanguageRow>) =>
    setRows((prev) => prev.map((r, k) => (k === i ? { ...r, ...next } : r)))
  const setDefault = (i: number) =>
    setRows((prev) => prev.map((r, k) => ({ ...r, default: k === i, prefix: k === i ? '' : r.prefix })))
  const remove = (i: number) => setRows((prev) => prev.filter((_, k) => k !== i))

  const submit = async () => {
    try {
      const res = await save.mutateAsync(rows)
      setRows((res.languages ?? rows).map((r) => ({ ...r })))
      toast.success(res.needRestart ? t('language.savedRestart') : t('language.saved'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  if (isLoading) return <Skeleton className="h-64 w-full" />

  return (
    <div className="flex flex-col gap-3">
        {rows.map((row, i) => (
          <Card key={`${row.code}-${i}`} className="flex flex-col gap-3 p-4">
            <div className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                <span className="font-mono text-sm font-medium">{row.code || '…'}</span>
                {row.default ? <Badge variant="success">{t('language.defaultTag')}</Badge> : null}
                <Badge variant={row.dir === 'rtl' ? 'secondary' : 'outline'}>{row.dir}</Badge>
              </div>
              <Can perm="language:update">
                <div className="flex items-center gap-3">
                  <label className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Switch checked={row.default} onCheckedChange={() => setDefault(i)} aria-label={t('language.default')} />
                    {t('language.default')}
                  </label>
                  <Button type="button" variant="ghost" size="icon" aria-label={t('language.remove')} onClick={() => remove(i)}>
                    <Trash2 aria-hidden="true" className="text-destructive" />
                  </Button>
                </div>
              </Can>
            </div>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              <Field label={t('language.fields.code')}>
                <Input value={row.code} className="font-mono" placeholder="ja-JP" onChange={(e) => patch(i, { code: e.target.value })} />
              </Field>
              <Field label={t('language.fields.name')}>
                <Input value={row.name} placeholder="日本語" onChange={(e) => patch(i, { name: e.target.value })} />
              </Field>
              <Field label={t('language.fields.prefix')} hint={row.default ? t('language.prefixRoot') : t('language.prefixHint')}>
                <Input
                  value={row.prefix ?? ''}
                  className="font-mono"
                  placeholder={row.default ? '' : 'ja'}
                  disabled={row.default}
                  onChange={(e) => patch(i, { prefix: e.target.value })}
                />
              </Field>
              <Field label={t('language.fields.htmlLang')} hint={t('language.derivedHint')}>
                <Input value={row.htmlLang ?? ''} className="font-mono" placeholder={row.code} onChange={(e) => patch(i, { htmlLang: e.target.value })} />
              </Field>
              <Field label={t('language.fields.ogLocale')} hint={t('language.derivedHint')}>
                <Input value={row.ogLocale ?? ''} className="font-mono" placeholder={row.code?.replace('-', '_')} onChange={(e) => patch(i, { ogLocale: e.target.value })} />
              </Field>
              <Field label={t('language.fields.dateFormat')} hint={t('language.dateFormatHint')}>
                <Input value={row.dateFormat ?? ''} className="font-mono" placeholder="2006-01-02" onChange={(e) => patch(i, { dateFormat: e.target.value })} />
              </Field>
            </div>
          </Card>
        ))}
        <Can perm="language:update">
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" variant="outline" onClick={() => setRows((prev) => [...prev, emptyRow()])}>
              <Plus aria-hidden="true" />
              {t('language.add')}
            </Button>
            <Button type="button" onClick={() => void submit()} disabled={save.isPending}>
              {t('common.save')}
            </Button>
            <p className="text-xs text-muted-foreground">{t('language.hint')}</p>
          </div>
        </Can>
    </div>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="flex min-w-0 flex-col gap-1 text-xs">
      <span className="text-muted-foreground">{label}</span>
      {children}
      {hint ? <span className="text-[11px] text-muted-foreground">{hint}</span> : null}
    </label>
  )
}
