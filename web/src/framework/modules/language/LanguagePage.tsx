import { useEffect, useMemo, useState } from 'react'
import { ChevronDown, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Skeleton } from '@/framework/ui/skeleton'
import { cn } from '@/lib/utils'
import { useAdminLanguages, useSaveLanguages, type LanguageRow } from './api'

const emptyRow = (): LanguageRow => ({
  code: '', name: '', dir: 'ltr', default: false, htmlLang: '', ogLocale: '', prefix: '', dateFormat: '',
})

// 保存是整表替换：用固定字段顺序做指纹，避免对象键序不同造成“永远显示未保存”。
const signature = (rows: LanguageRow[]) =>
  rows
    .map((r) => [r.code, r.name, r.dir, r.default ? '1' : '0', r.htmlLang, r.ogLocale, r.prefix, r.dateFormat].join('\u0001'))
    .join('\u0002')

const CODE_RE = /^[a-zA-Z]{2,3}(-[a-zA-Z0-9]+)*$/
const PREFIX_RE = /^[a-z0-9]+(-[a-z0-9]+)*$/

/** 一行语言的问题；空串表示没问题。 */
function rowError(r: LanguageRow, rows: LanguageRow[], index: number, dupMsg: string, codeMsg: string, prefixMsg: string) {
  const code = r.code.trim()
  if (!CODE_RE.test(code)) return codeMsg
  if (rows.some((o, i) => i !== index && o.code.trim().toLowerCase() === code.toLowerCase())) return dupMsg
  if (!r.default && r.prefix && !PREFIX_RE.test(r.prefix.trim())) return prefixMsg
  return ''
}

/**
 * 平台语言清单：全站唯一的语言真源。
 *
 * 这里是整表替换（与后端一致），所以编辑发生在本地副本上，点保存才落库。
 * 前缀或语言集合变了需要重启才对公开站生效——后端会算，界面照实说，不让人白重启。
 * 日常只改「显示名称」，hreflang / og:locale / 日期格式是派生值，收在每张卡片的高级里。
 */
export function LanguageSection() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data, isLoading } = useAdminLanguages()
  const save = useSaveLanguages()
  const [rows, setRows] = useState<LanguageRow[]>([])

  useEffect(() => {
    if (data) setRows(data.map((r) => ({ ...r })))
  }, [data])

  const baseline = useMemo(() => signature(data ?? []), [data])
  const dirty = signature(rows) !== baseline

  const patch = (i: number, next: Partial<LanguageRow>) =>
    setRows((prev) => prev.map((r, k) => (k === i ? { ...r, ...next } : r)))
  const setDefault = (i: number) =>
    setRows((prev) => prev.map((r, k) => ({ ...r, default: k === i, prefix: k === i ? '' : r.prefix })))

  const remove = async (i: number) => {
    const row = rows[i]
    const ok = await confirm({
      destructive: true,
      description: t('language.removeConfirm', { name: row.name || row.code || '—' }),
    })
    if (ok) setRows((prev) => prev.filter((_, k) => k !== i))
  }

  const firstError = rows
    .map((r, i) => rowError(r, rows, i, t('language.codeDup'), t('language.codeInvalid'), t('language.prefixInvalid')))
    .find((e) => e !== '')

  const submit = async () => {
    if (firstError) {
      toast.error(firstError)
      return
    }
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
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted-foreground">{t('language.desc')}</p>
        <Can perm="language:update">
          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" onClick={() => setRows((prev) => [...prev, emptyRow()])}>
              <Plus aria-hidden="true" />
              {t('language.add')}
            </Button>
            <Button type="button" onClick={() => void submit()} disabled={save.isPending || !dirty}>
              {t('common.save')}
            </Button>
          </div>
        </Can>
      </div>

      {rows.map((row, i) => {
        const err = rowError(row, rows, i, t('language.codeDup'), t('language.codeInvalid'), t('language.prefixInvalid'))
        return (
          <Card key={`${row.code}-${i}`} className="flex flex-col gap-3 p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate font-mono text-sm font-medium">{row.code || '…'}</span>
                {row.default ? <Badge variant="success">{t('language.defaultTag')}</Badge> : null}
                <Badge variant={row.dir === 'rtl' ? 'secondary' : 'outline'}>{row.dir}</Badge>
              </div>
              <Can perm="language:update">
                <div className="flex items-center gap-2">
                  {row.default ? null : (
                    <Button type="button" variant="ghost" size="sm" onClick={() => setDefault(i)}>
                      {t('language.setDefault')}
                    </Button>
                  )}
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={row.default ? t('language.removeDefaultBlocked') : t('language.remove')}
                    disabled={row.default}
                    onClick={() => void remove(i)}
                  >
                    <Trash2 aria-hidden="true" className="text-destructive" />
                  </Button>
                </div>
              </Can>
            </div>

            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              <Field label={t('language.fields.code')} error={err}>
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
            </div>

            {/* 派生字段：留空自动推导，平时不用动；有自定义值时标题上标出来，免得藏在折叠里被忘掉 */}
            <details className="group rounded-md border border-border/60 px-3 py-2">
              <summary className="flex cursor-pointer list-none items-center gap-1.5 text-xs text-muted-foreground [&::-webkit-details-marker]:hidden">
                <ChevronDown aria-hidden="true" className="size-3.5 transition-transform group-open:rotate-180" />
                {t('language.advanced')}
              </summary>
              <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
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
            </details>
          </Card>
        )
      })}

      <p className="text-xs text-muted-foreground">{t('language.hint')}</p>
    </div>
  )
}

function Field({ label, hint, error, children }: { label: string; hint?: string; error?: string; children: React.ReactNode }) {
  return (
    <label className={cn('flex min-w-0 flex-col gap-1 text-xs')}>
      <span className="text-muted-foreground">{label}</span>
      {children}
      {error ? <span className="text-destructive">{error}</span> : hint ? <span className="text-[11px] text-muted-foreground">{hint}</span> : null}
    </label>
  )
}
