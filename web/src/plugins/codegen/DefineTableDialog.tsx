import { useEffect, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { REQUIRABLE, type FieldDef, type FieldKind, type TableDef } from './api'

const NAME_RE = /^[a-z][a-z0-9_]{0,43}$/
const IDENT_RE = /^[a-z][a-z0-9_]{0,47}$/
const MIGRATION_RE = /^\d{1,4}$/
const RESERVED = new Set(['id', 'tenant_id', 'created_at', 'updated_at', 'deleted_at'])
const KINDS: FieldKind[] = ['string', 'text', 'int', 'float', 'bool', 'date', 'datetime']

let seq = 0

interface Row {
  id: number
  name: string
  kind: FieldKind
  length: string
  required: boolean
  comment: string
}

function blankRow(): Row {
  seq += 1
  return { id: seq, name: '', kind: 'string', length: '255', required: false, comment: '' }
}

interface Props {
  open: boolean
  onOpenChange: (v: boolean) => void
  pending: boolean
  onSubmit: (def: TableDef) => void
}

/** 新建业务表：只定义结构，zip 里附带三引擎迁移，插件不写库。 */
export function DefineTableDialog({ open, onOpenChange, pending, onSubmit }: Props) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [tenant, setTenant] = useState(true)
  const [softDelete, setSoftDelete] = useState(false)
  const [migration, setMigration] = useState('001')
  const [rows, setRows] = useState<Row[]>([blankRow()])
  const [error, setError] = useState<string>()

  useEffect(() => {
    if (!open) return
    setName('')
    setTenant(true)
    setSoftDelete(false)
    setMigration('001')
    setRows([blankRow()])
    setError(undefined)
  }, [open])

  const patch = (id: number, p: Partial<Row>) => setRows((cur) => cur.map((r) => (r.id === id ? { ...r, ...p } : r)))
  const remove = (id: number) => setRows((cur) => (cur.length <= 1 ? cur : cur.filter((r) => r.id !== id)))

  const submit = () => {
    const base = name.trim().replace(/^biz_/, '')
    if (!NAME_RE.test(base)) return setError('errName')
    if (!MIGRATION_RE.test(migration.trim()) || Number(migration) === 0) return setError('errMigration')
    if (rows.length === 0) return setError('errFields')
    const seen = new Set<string>()
    for (const r of rows) {
      const n = r.name.trim()
      if (!IDENT_RE.test(n)) return setError('errFieldName')
      if (RESERVED.has(n)) return setError('errReserved')
      if (seen.has(n)) return setError('errFieldDup')
      seen.add(n)
      if (r.kind === 'string') {
        const len = Number(r.length)
        if (!Number.isInteger(len) || len < 1 || len > 4000) return setError('errLength')
      }
    }
    const fields: FieldDef[] = rows.map((r) => ({
      name: r.name.trim(),
      kind: r.kind,
      required: r.required && REQUIRABLE.includes(r.kind),
      length: r.kind === 'string' ? Number(r.length) : 0,
      comment: r.comment.trim() || undefined,
    }))
    setError(undefined)
    onSubmit({ name: base, tenant, softDelete, migration: migration.trim(), fields })
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="xl"
      title={t('plugin_codegen.define.title')}
      description={t('plugin_codegen.define.description')}
      footer={
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            {t('plugin_codegen.define.cancel')}
          </Button>
          <Button onClick={submit} disabled={pending}>
            {t('plugin_codegen.define.submit')}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_10rem]">
          <div className="flex flex-col gap-2">
            <Label htmlFor="codegen-def-name">{t('plugin_codegen.define.name')}</Label>
            <div className="flex items-center gap-2">
              <span className="font-mono text-sm text-muted-foreground">biz_</span>
              <Input
                id="codegen-def-name"
                value={name}
                onChange={(e) => setName(e.target.value.trim().toLowerCase())}
                placeholder="product"
                maxLength={44}
                autoComplete="off"
              />
            </div>
            <p className="text-xs text-muted-foreground">{t('plugin_codegen.define.nameHint')}</p>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="codegen-def-migration">{t('plugin_codegen.define.migration')}</Label>
            <Input
              id="codegen-def-migration"
              value={migration}
              onChange={(e) => setMigration(e.target.value.trim())}
              placeholder="001"
              inputMode="numeric"
              maxLength={4}
            />
            <p className="text-xs text-muted-foreground">{t('plugin_codegen.define.migrationHint')}</p>
          </div>
        </div>

        <div className="flex flex-col gap-2 md:flex-row md:gap-6">
          <div className="flex items-center gap-2">
            <Checkbox id="codegen-def-tenant" checked={tenant} onCheckedChange={(v) => setTenant(v === true)} />
            <Label htmlFor="codegen-def-tenant">{t('plugin_codegen.define.tenant')}</Label>
          </div>
          <div className="flex items-center gap-2">
            <Checkbox id="codegen-def-soft-delete" checked={softDelete} onCheckedChange={(v) => setSoftDelete(v === true)} />
            <Label htmlFor="codegen-def-soft-delete">{t('plugin_codegen.define.softDelete')}</Label>
          </div>
        </div>

        <section className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Label>{t('plugin_codegen.define.fields')}</Label>
            <Button variant="outline" size="sm" onClick={() => setRows((cur) => [...cur, blankRow()])}>
              <Plus aria-hidden="true" />
              {t('plugin_codegen.define.addField')}
            </Button>
          </div>
          <div className="divide-y divide-border rounded-lg border border-border">
            <div className="hidden gap-2 px-3 py-2 text-xs text-muted-foreground md:grid md:grid-cols-[minmax(0,1fr)_7rem_5rem_minmax(0,1fr)_4rem_auto]">
              <span>{t('plugin_codegen.define.fieldName')}</span>
              <span>{t('plugin_codegen.define.fieldType')}</span>
              <span>{t('plugin_codegen.define.fieldLength')}</span>
              <span>{t('plugin_codegen.define.fieldComment')}</span>
              <span className="text-center">{t('plugin_codegen.define.fieldRequired')}</span>
              <span className="w-9" />
            </div>
            {rows.map((r) => (
              <div key={r.id} className="grid gap-2 px-3 py-2 md:grid-cols-[minmax(0,1fr)_7rem_5rem_minmax(0,1fr)_4rem_auto] md:items-center">
                <Input
                  value={r.name}
                  onChange={(e) => patch(r.id, { name: e.target.value.trim().toLowerCase() })}
                  placeholder={t('plugin_codegen.define.fieldName')}
                  aria-label={t('plugin_codegen.define.fieldName')}
                  maxLength={48}
                  autoComplete="off"
                />
                <Select
                  value={r.kind}
                  onValueChange={(v) =>
                    patch(r.id, {
                      kind: v as FieldKind,
                      required: REQUIRABLE.includes(v as FieldKind) ? r.required : false,
                      length: v === 'string' ? r.length || '255' : '',
                    })
                  }
                >
                  <SelectTrigger aria-label={t('plugin_codegen.define.fieldType')}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {KINDS.map((k) => (
                      <SelectItem key={k} value={k}>
                        {t(`plugin_codegen.define.kinds.${k}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Input
                  type="number"
                  value={r.length}
                  onChange={(e) => patch(r.id, { length: e.target.value })}
                  placeholder="255"
                  aria-label={t('plugin_codegen.define.fieldLength')}
                  disabled={r.kind !== 'string'}
                  min={1}
                  max={4000}
                />
                <Input
                  value={r.comment}
                  onChange={(e) => patch(r.id, { comment: e.target.value })}
                  placeholder={t('plugin_codegen.define.fieldComment')}
                  aria-label={t('plugin_codegen.define.fieldComment')}
                  maxLength={200}
                />
                <div className="flex items-center gap-2 md:justify-center">
                  <Checkbox
                    checked={r.required}
                    disabled={!REQUIRABLE.includes(r.kind)}
                    onCheckedChange={(v) => patch(r.id, { required: v === true })}
                    aria-label={t('plugin_codegen.define.fieldRequired')}
                  />
                  <span className="text-xs text-muted-foreground md:hidden">{t('plugin_codegen.define.fieldRequired')}</span>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => remove(r.id)}
                  disabled={rows.length <= 1}
                  aria-label={t('plugin_codegen.define.removeField')}
                >
                  <Trash2 aria-hidden="true" />
                </Button>
              </div>
            ))}
          </div>
        </section>

        {error ? <p className="text-sm text-destructive">{t(`plugin_codegen.define.${error}`)}</p> : null}
      </div>
    </ResponsiveDialog>
  )
}
