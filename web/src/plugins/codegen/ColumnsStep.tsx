import { useState } from 'react'
import { Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FormGrid, FormLayout } from '@/framework/components/FormLayout'
import { RequiredMark } from '@/framework/components/RequiredMark'
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

  // 系统列（id/tenant_id/时间戳）不可配置、每张表必然存在，默认收起——不收起的话
  // 四行灰行 × 四个禁用空勾选框把真正要配置的业务行淹没在一排排短横线里。
  const [showSystem, setShowSystem] = useState(false)
  const rows = showSystem ? schema.columns : schema.columns.filter((c) => !c.system)

  // 系统列/不支持的列不做勾选，与控件列一致画短横线：一排能点但禁用的空框，比 "-" 更吵。
  const flag = (key: 'list' | 'query' | 'form' | 'required', allow: (c: ColumnInfo, cur: ColumnConfig) => boolean): Column<ColumnInfo> => ({
    key,
    title: t(`plugin_codegen.col.${key}`),
    align: 'center',
    width: 88,
    render: (c) => {
      const cur = byName.get(c.name)
      if (!cur || c.system || !c.supported) return <span className="text-muted-foreground">-</span>
      if (!allow(c, cur)) return <span className="text-muted-foreground">-</span>
      return (
        <Checkbox
          checked={cur[key]}
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
      width: 200,
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
      width: 150,
      mobile: 'sub',
      render: (c) => <span className="font-mono text-xs text-muted-foreground">{c.type}{c.nullable ? ' · NULL' : ''}</span>,
    },
    { key: 'comment', title: t('plugin_codegen.col.comment'), mobile: 'hidden', render: (c) => c.comment },
    flag('list', () => true),
    flag('query', (c) => QUERYABLE.includes(c.kind)),
    flag('form', () => true),
    flag('required', (c, cur) => REQUIRABLE.includes(c.kind) && cur.form),
    {
      key: 'control',
      title: t('plugin_codegen.col.control'),
      width: 190,
      render: (c) => {
        const cur = byName.get(c.name)
        const options = CONTROLS[c.kind] ?? []
        if (!cur || !c.supported || c.system || !cur.form || options.length === 0) return <span className="text-muted-foreground">-</span>
        return (
          <div className="flex items-center gap-1.5">
            <Select value={cur.control || options[0]} onValueChange={(v) => patch(c.name, { control: v as Control, dict: v === 'dict' ? (cur.dict || '') : undefined })} disabled={options.length < 2}>
              <SelectTrigger className="w-36" aria-label={`${c.name} ${t('plugin_codegen.col.control')}`}>
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
            {cur.control === 'dict' ? (
              <Input
                className="w-32 font-mono"
                value={cur.dict ?? ''}
                onChange={(e) => patch(c.name, { dict: e.target.value.trim().toLowerCase() })}
                placeholder="sys_status"
                aria-label={`${c.name} ${t('plugin_codegen.col.dictCode')}`}
                maxLength={48}
              />
            ) : null}
          </div>
        )
      },
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      <FormLayout onSubmit={(e) => e.preventDefault()}>
        <FormGrid cols={2}>
          <div className="flex flex-col gap-2">
            <Label htmlFor="codegen-module">
              {t('plugin_codegen.config.module')}
              <RequiredMark />
            </Label>
            <Input id="codegen-module" value={module} onChange={(e) => onModuleChange(e.target.value.trim())} maxLength={31} aria-invalid={!!moduleError} />
            <p className={moduleError ? 'text-xs text-destructive' : 'text-xs text-muted-foreground'}>{moduleError ?? t('plugin_codegen.config.moduleHint')}</p>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="codegen-gomodule">{t('plugin_codegen.config.goModule')}</Label>
            <Input id="codegen-gomodule" value={goModule} onChange={(e) => onGoModuleChange(e.target.value.trim())} maxLength={100} placeholder="example.com/shop" aria-invalid={!!goModuleError} />
            <p className={goModuleError ? 'text-xs text-destructive' : 'text-xs text-muted-foreground'}>{goModuleError ?? t('plugin_codegen.config.goModuleHint')}</p>
          </div>
        </FormGrid>
      </FormLayout>
      <DataTable
        columns={columns}
        data={rows}
        rowKey="name"
        pagination={false}
        columnSettingsKey="plugin-codegen-columns"
        emptyText={t('plugin_codegen.col.empty')}
        toolbar={
          <>
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Info aria-hidden="true" className="size-3.5 shrink-0" />
              {t('plugin_codegen.config.shape', { shape: t(`plugin_codegen.shape.${schema.shape}`) })}
            </p>
            <label className="flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground" title={t('plugin_codegen.col.showSystemHint')}>
              <Checkbox checked={showSystem} onCheckedChange={(v) => setShowSystem(v === true)} />
              {t('plugin_codegen.col.showSystem')}
            </label>
          </>
        }
      />
    </div>
  )
}
