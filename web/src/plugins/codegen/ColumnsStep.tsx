import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FormGrid, FormLayout } from '@/framework/components/FormLayout'
import { Badge } from '@/framework/ui/badge'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { CONTROLS, QUERYABLE, REQUIRABLE, type ColumnConfig, type ColumnInfo, type Control, type TableSchema } from './api'

interface Props {
  schema: TableSchema
  cfg: ColumnConfig[]
  onCfgChange: (next: ColumnConfig[]) => void
  module: string
  onModuleChange: (v: string) => void
  goModule: string
  onGoModuleChange: (v: string) => void
  moduleError?: string
  goModuleError?: string
}

export function ColumnsStep({ schema, cfg, onCfgChange, module, onModuleChange, goModule, onGoModuleChange, moduleError, goModuleError }: Props) {
  const { t } = useTranslation()
  const byName = new Map(cfg.map((c) => [c.name, c]))
  const patch = (name: string, p: Partial<ColumnConfig>) =>
    onCfgChange(cfg.map((c) => (c.name === name ? { ...c, ...p, ...(p.form === false ? { required: false } : {}) } : c)))

  const flag = (key: 'list' | 'query' | 'form' | 'required', allow: (c: ColumnInfo, cur: ColumnConfig) => boolean): Column<ColumnInfo> => ({
    key,
    title: t(`plugin_codegen.col.${key}`),
    align: 'center',
    render: (c) => {
      const cur = byName.get(c.name)
      if (!cur) return null
      return (
        <Checkbox
          checked={cur[key]}
          disabled={!c.supported || c.system || !allow(c, cur)}
          onCheckedChange={(v) => patch(c.name, { [key]: v === true })}
          aria-label={`${c.name} ${t(`plugin_codegen.col.${key}`)}`}
        />
      )
    },
  })

  const columns: Column<ColumnInfo>[] = [
    {
      key: 'name',
      title: t('plugin_codegen.col.name'),
      mobile: 'title',
      render: (c) => (
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-sm">{c.name}</span>
          {c.primaryKey ? <Badge variant="info">PK</Badge> : null}
          {c.system ? <Badge variant="secondary">{t('plugin_codegen.col.system')}</Badge> : null}
          {!c.supported ? <Badge variant="warning">{t('plugin_codegen.col.unsupported')}</Badge> : null}
        </span>
      ),
    },
    {
      key: 'type',
      title: t('plugin_codegen.col.type'),
      mobile: 'sub',
      render: (c) => <span className="font-mono text-xs text-muted-foreground">{c.type}{c.nullable ? ' · NULL' : ''}</span>,
    },
    { key: 'comment', title: t('plugin_codegen.col.comment'), mobile: 'hidden', render: (c) => c.comment || '-' },
    flag('list', () => true),
    flag('query', (c) => QUERYABLE.includes(c.kind)),
    flag('form', () => true),
    flag('required', (c, cur) => REQUIRABLE.includes(c.kind) && cur.form),
    {
      key: 'control',
      title: t('plugin_codegen.col.control'),
      render: (c) => {
        const cur = byName.get(c.name)
        const options = CONTROLS[c.kind] ?? []
        if (!cur || !c.supported || c.system || !cur.form || options.length === 0) return <span className="text-muted-foreground">-</span>
        return (
          <Select value={cur.control || options[0]} onValueChange={(v) => patch(c.name, { control: v as Control })} disabled={options.length < 2}>
            <SelectTrigger className="w-32" aria-label={`${c.name} ${t('plugin_codegen.col.control')}`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {options.map((o) => (
                <SelectItem key={o} value={o}>
                  {t(`plugin_codegen.controls.${o}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )
      },
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      <FormLayout onSubmit={(e) => e.preventDefault()}>
        <FormGrid cols={2}>
          <div className="flex flex-col gap-2">
            <Label htmlFor="codegen-module">{t('plugin_codegen.config.module')}</Label>
            <Input id="codegen-module" value={module} onChange={(e) => onModuleChange(e.target.value.trim())} maxLength={31} aria-invalid={!!moduleError} />
            <p className={moduleError ? 'text-xs text-destructive' : 'text-xs text-muted-foreground'}>{moduleError ?? t('plugin_codegen.config.moduleHint')}</p>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="codegen-gomodule">{t('plugin_codegen.config.goModule')}</Label>
            <Input id="codegen-gomodule" value={goModule} onChange={(e) => onGoModuleChange(e.target.value.trim())} maxLength={100} placeholder="nexadmin" aria-invalid={!!goModuleError} />
            <p className={goModuleError ? 'text-xs text-destructive' : 'text-xs text-muted-foreground'}>{goModuleError ?? t('plugin_codegen.config.goModuleHint')}</p>
          </div>
        </FormGrid>
        <p className="text-xs text-muted-foreground">
          {t('plugin_codegen.config.shape', { shape: t(`plugin_codegen.shape.${schema.shape}`) })}
        </p>
      </FormLayout>
      <DataTable
        columns={columns}
        data={schema.columns}
        rowKey="name"
        pagination={false}
        columnSettingsKey="plugin-codegen-columns"
        emptyText={t('plugin_codegen.col.empty')}
      />
    </div>
  )
}
