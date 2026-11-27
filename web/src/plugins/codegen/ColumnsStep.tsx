import { useState } from 'react'
import { ArrowDown, ArrowUp, Info, Settings2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FormGrid, FormLayout } from '@/framework/components/FormLayout'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { CONTROLS, QUERYABLE, QUERY_OPS, REQUIRABLE, type ColumnConfig, type ColumnInfo, type Control, type GenProfile, type QueryOp, type TableSchema } from './api'

interface Props {
  schema: TableSchema
  cfg: ColumnConfig[]
  onCfgChange: (next: ColumnConfig[]) => void
  module: string
  onModuleChange: (v: string) => void
  goModule: string
  onGoModuleChange: (v: string) => void
  profile: GenProfile
  onProfileChange: (v: GenProfile) => void
  moduleError?: string
  goModuleError?: string
}

export function ColumnsStep({ schema, cfg, onCfgChange, module, onModuleChange, goModule, onGoModuleChange, profile, onProfileChange, moduleError, goModuleError }: Props) {
  const { t } = useTranslation()
  const byName = new Map(cfg.map((c) => [c.name, c]))
  const patch = (name: string, p: Partial<ColumnConfig>) =>
    onCfgChange(cfg.map((c) => (c.name === name ? { ...c, ...p, ...(p.form === false ? { required: false } : {}) } : c)))

  // 系统列（id/tenant_id/时间戳）不可配置、每张表必然存在，默认收起——不收起的话
  // 四行灰行 × 四个禁用空勾选框把真正要配置的业务行淹没在一排排短横线里。
  const [showSystem, setShowSystem] = useState(false)
  const [metadata, setMetadata] = useState<{ name: string; value: ColumnConfig }>()
  const metadataColumn = metadata ? schema.columns.find((c) => c.name === metadata.name) : undefined
  const patchMetadata = (p: Partial<ColumnConfig>) =>
    setMetadata((cur) => (cur ? { ...cur, value: { ...cur.value, ...p } } : cur))
  // 行顺序跟生成物字段顺序走（sort）：上移 / 下移后表格当场重排，用户能直接看到结果。
  const ordered = [...schema.columns].sort(
    (a, b) => (byName.get(a.name)?.sort ?? 0) - (byName.get(b.name)?.sort ?? 0),
  )
  const rows = showSystem ? ordered : ordered.filter((c) => !c.system)
  // 上移 / 下移按当前显示顺序算，而不是 table 原始列顺序：移动后端点要跟着换。
  const business = ordered.filter((c) => c.supported && !c.system).map((c) => c.name)

  const move = (name: string, dir: -1 | 1) => {
    const target = business[business.indexOf(name) + dir]
    if (!target) return
    const a = cfg.findIndex((c) => c.name === name)
    const b = cfg.findIndex((c) => c.name === target)
    if (a < 0 || b < 0) return
    const next = [...cfg]
    ;[next[a], next[b]] = [next[b], next[a]]
    onCfgChange(next.map((c, i) => ({ ...c, sort: i + 1 })))
  }

  // 系统列/不支持的列不做勾选，与控件列一致画短横线：一排能点但禁用的空框，比 "-" 更吵。
  const flag = (key: 'list' | 'query' | 'form' | 'required', allow: (c: ColumnInfo, cur: ColumnConfig) => boolean): Column<ColumnInfo> => ({
    key,
    title: t(`plugin_codegen.col.${key}`),
    align: 'center',
    width: 76,
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
      width: 180,
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
      width: 120,
      mobile: 'sub',
      render: (c) => <span className="font-mono text-xs text-muted-foreground">{c.type}{c.nullable ? ' · NULL' : ''}</span>,
    },
    { key: 'comment', title: t('plugin_codegen.col.comment'), mobile: 'hidden', render: (c) => c.comment },
    flag('list', () => true),
    flag('query', (c) => QUERYABLE.includes(c.kind)),
    flag('form', () => true),
    flag('required', (c, cur) => REQUIRABLE.includes(c.kind) && cur.form),
    {
      key: 'queryOp',
      title: t('plugin_codegen.col.queryOp'),
      width: 132,
      render: (c) => {
        const cur = byName.get(c.name)
        if (!cur || !c.supported || c.system || !cur.query) return <span className="text-muted-foreground">-</span>
        const ops = QUERY_OPS[c.kind] ?? []
        if (ops.length === 0) return <span className="text-muted-foreground">-</span>
        if (ops.length === 1) return <span className="text-xs text-muted-foreground">{t(`plugin_codegen.queryOps.${ops[0]}`)}</span>
        return (
          <Select value={cur.queryOp || ops[0]} onValueChange={(v) => patch(c.name, { queryOp: v as QueryOp })}>
            <SelectTrigger className="w-28" aria-label={`${c.name} ${t('plugin_codegen.col.queryOp')}`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ops.map((op) => (
                <SelectItem key={op} value={op}>
                  {t(`plugin_codegen.queryOps.${op}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )
      },
    },
    {
      key: 'control',
      title: t('plugin_codegen.col.control'),
      width: 260,
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
                className="w-40 font-mono"
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
    {
      key: 'sort',
      title: t('plugin_codegen.col.sort'),
      align: 'center',
      width: 76,
      render: (c) => {
        if (!c.supported || c.system) return <span className="text-muted-foreground">-</span>
        const i = business.indexOf(c.name)
        return (
          <div className="flex items-center justify-center gap-0.5">
            <Button
              variant="ghost"
              size="icon"
              className="size-7"
              disabled={i <= 0}
              aria-label={`${c.name} ${t('plugin_codegen.col.moveUp')}`}
              title={t('plugin_codegen.col.moveUp')}
              onClick={() => move(c.name, -1)}
            >
              <ArrowUp aria-hidden="true" className="size-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="size-7"
              disabled={i < 0 || i >= business.length - 1}
              aria-label={`${c.name} ${t('plugin_codegen.col.moveDown')}`}
              title={t('plugin_codegen.col.moveDown')}
              onClick={() => move(c.name, 1)}
            >
              <ArrowDown aria-hidden="true" className="size-3.5" />
            </Button>
          </div>
        )
      },
    },
    {
      key: 'metadata',
      title: t('plugin_codegen.col.metadata'),
      mobile: 'sub',
      align: 'center',
      width: 76,
      render: (c) => {
        const cur = byName.get(c.name)
        if (!cur || !c.supported || c.system) return <span className="text-muted-foreground">-</span>
        return (
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            aria-label={`${c.name} ${t('plugin_codegen.col.metadata')}`}
            title={t('plugin_codegen.col.metadata')}
            onClick={() => setMetadata({ name: c.name, value: { ...cur } })}
          >
            <Settings2 aria-hidden="true" className="size-3.5" />
          </Button>
        )
      },
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      <FormLayout onSubmit={(e) => e.preventDefault()}>
        <FormGrid cols={1}>
          <div className="flex flex-col gap-2">
            <Label htmlFor="codegen-profile">{t('plugin_codegen.config.profile')}</Label>
            <Select value={profile} onValueChange={(v) => onProfileChange(v as GenProfile)}>
              <SelectTrigger id="codegen-profile" aria-label={t('plugin_codegen.config.profile')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(['full', 'backend', 'frontend'] as const).map((p) => (
                  <SelectItem key={p} value={p}>{t(`plugin_codegen.profiles.${p}`)}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{t('plugin_codegen.config.profileHint')}</p>
          </div>
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
            <Input id="codegen-gomodule" value={goModule} onChange={(e) => onGoModuleChange(e.target.value.trim())} maxLength={100} placeholder="example.com/shop" aria-invalid={!!goModuleError} disabled={profile === 'frontend'} />
            <p className={goModuleError && profile !== 'frontend' ? 'text-xs text-destructive' : 'text-xs text-muted-foreground'}>{profile === 'frontend' ? t('plugin_codegen.config.frontendOnlyHint') : goModuleError ?? t('plugin_codegen.config.goModuleHint')}</p>
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
      <ResponsiveDialog
        side="right"
        open={!!metadata}
        onOpenChange={(open) => { if (!open) setMetadata(undefined) }}
        title={t('plugin_codegen.metadata.title', { name: metadata?.name ?? '' })}
        description={t('plugin_codegen.metadata.hint')}
        footer={
          <>
            <Button variant="outline" onClick={() => setMetadata(undefined)}>{t('common.cancel')}</Button>
            <Button type="submit" form="codegen-field-metadata">{t('common.save')}</Button>
          </>
        }
      >
        {metadata ? (
          <FormLayout
            id="codegen-field-metadata"
            onSubmit={(e) => {
              e.preventDefault()
              patch(metadata.name, metadata.value)
              setMetadata(undefined)
            }}
          >
            <FormGrid cols={1}>
              <div className="flex flex-col gap-2">
                <Label htmlFor="codegen-meta-label">{t('plugin_codegen.metadata.label')}</Label>
                <Input id="codegen-meta-label" value={metadata.value.label ?? ''} onChange={(e) => patchMetadata({ label: e.target.value })} maxLength={120} />
                <p className="text-xs text-muted-foreground">{t('plugin_codegen.metadata.labelHint')}</p>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="codegen-meta-placeholder">{t('plugin_codegen.metadata.placeholder')}</Label>
                <Input id="codegen-meta-placeholder" value={metadata.value.placeholder ?? ''} onChange={(e) => patchMetadata({ placeholder: e.target.value })} maxLength={200} />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="codegen-meta-default">{t('plugin_codegen.metadata.defaultValue')}</Label>
                {metadataColumn?.kind === 'bool' ? (
                  <Select value={metadata.value.defaultValue || '__empty__'} onValueChange={(v) => patchMetadata({ defaultValue: v === '__empty__' ? '' : v })}>
                    <SelectTrigger id="codegen-meta-default"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__empty__">{t('plugin_codegen.metadata.noDefault')}</SelectItem>
                      <SelectItem value="true">{t('common.yes')}</SelectItem>
                      <SelectItem value="false">{t('common.no')}</SelectItem>
                    </SelectContent>
                  </Select>
                ) : (
                  <Input id="codegen-meta-default" value={metadata.value.defaultValue ?? ''} onChange={(e) => patchMetadata({ defaultValue: e.target.value })} maxLength={500} />
                )}
                <p className="text-xs text-muted-foreground">{t('plugin_codegen.metadata.defaultHint')}</p>
              </div>
              {metadataColumn?.kind === 'string' || metadataColumn?.kind === 'text' ? (
                <>
                  <div className="flex flex-col gap-2">
                    <Label htmlFor="codegen-meta-min">{t('plugin_codegen.metadata.minLength')}</Label>
                    <Input id="codegen-meta-min" type="number" min={0} max={4000} value={metadata.value.minLength ?? 0} onChange={(e) => patchMetadata({ minLength: Number(e.target.value) })} />
                  </div>
                  <div className="flex flex-col gap-2">
                    <Label htmlFor="codegen-meta-max">{t('plugin_codegen.metadata.maxLength')}</Label>
                    <Input id="codegen-meta-max" type="number" min={0} max={metadataColumn.length ?? 4000} value={metadata.value.maxLength ?? 0} onChange={(e) => patchMetadata({ maxLength: Number(e.target.value) })} />
                  </div>
                  <div className="flex flex-col gap-2">
                    <Label htmlFor="codegen-meta-pattern">{t('plugin_codegen.metadata.pattern')}</Label>
                    <Input id="codegen-meta-pattern" className="font-mono" value={metadata.value.pattern ?? ''} onChange={(e) => patchMetadata({ pattern: e.target.value })} maxLength={200} placeholder="^[A-Z]{2}[0-9]+$" />
                    <p className="text-xs text-muted-foreground">{t('plugin_codegen.metadata.patternHint')}</p>
                  </div>
                </>
              ) : null}
            </FormGrid>
          </FormLayout>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
}
