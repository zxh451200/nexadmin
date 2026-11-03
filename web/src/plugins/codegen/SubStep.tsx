import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { Badge } from '@/framework/ui/badge'
import { Checkbox } from '@/framework/ui/checkbox'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { CONTROLS, REQUIRABLE, useSchema, type ColumnConfig, type ColumnInfo, type SubTableSpec, type TableInfo, type TableSchema } from './api'

interface Props {
  tables: TableInfo[]
  /** 主表名（biz_ 前缀的库表名）；子表不能选它自己 */
  mainTable?: string
  value?: SubTableSpec
  onChange: (v: SubTableSpec | undefined) => void
}

/**
 * 主子表的可选子表配置：选一张已有表（须有指向主表的 BIGINT *_id 列），
 * 配置哪些列进子行编辑器。子表不能凭空定义——它已经有自己的建表迁移。
 */
export function SubStep({ tables, mainTable, value, onChange }: Props) {
  const { t } = useTranslation()
  const schema = useSchema()
  const [table, setTable] = useState(value?.table ?? '')
  const [sc, setSc] = useState<TableSchema>()

  useEffect(() => {
    setTable(value?.table ?? '')
  }, [value?.table])

  // 选表后拉子表结构：外键默认取第一个 *_id 的 BIGINT 列，列配置用推荐值。
  useEffect(() => {
    if (!table) {
      setSc(undefined)
      return
    }
    if (table === value?.table) {
      return // 载入已保存配置时不再重复拉取
    }
    let stale = false
    schema
      .mutateAsync({ table })
      .then((loaded) => {
        if (stale) return
        if (loaded.tree) {
          // 树形子表没法整存整取（子行自身还要挂父级），直接拒绝并复位。
          setSc(undefined)
          onChange(undefined)
          return
        }
        const fk = loaded.columns.find((c) => c.kind === 'id' && !c.system && c.name !== 'tenant_id')
        const spec: SubTableSpec = {
          table,
          foreignKey: fk?.name ?? '',
          columns: loaded.columns.map((c) => c.config),
        }
        setSc(loaded)
        onChange(spec)
      })
      .catch(() => {
        if (!stale) setSc(undefined)
      })
    return () => {
      stale = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [table])

  if (!mainTable) return null

  const candidates = tables.filter((x) => x.name !== mainTable)

  const patch = (name: string, p: Partial<ColumnConfig>) => {
    if (!value) return
    onChange({ ...value, columns: value.columns.map((c) => (c.name === name ? { ...c, ...p, ...(p.form === false ? { required: false } : {}) } : c)) })
  }

  const fkOptions = (sc?.columns ?? []).filter((c) => c.kind === 'id' && !c.system)

  const columns: Column<ColumnInfo>[] = [
    {
      key: 'name',
      title: t('plugin_codegen.col.name'),
      width: 200,
      mobile: 'title',
      render: (c) => (
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-sm">{c.name}</span>
          {c.system ? <Badge variant="secondary">{t('plugin_codegen.col.system')}</Badge> : null}
          {!c.supported ? <Badge variant="warning">{t('plugin_codegen.col.unsupported')}</Badge> : null}
        </span>
      ),
    },
    { key: 'comment', title: t('plugin_codegen.col.comment'), render: (c) => c.comment || '-' },
    {
      key: 'form',
      title: t('plugin_codegen.col.form'),
      align: 'center',
      width: 88,
      render: (c) => {
        if (!value || !c.supported || c.system) return null
        const cur = value.columns.find((x) => x.name === c.name)
        if (!cur) return null
        return (
          <Checkbox
            checked={cur.form}
            disabled={c.name === value.foreignKey}
            onCheckedChange={(v) => patch(c.name, { form: v === true })}
            aria-label={`${c.name} ${t('plugin_codegen.col.form')}`}
          />
        )
      },
    },
    {
      key: 'required',
      title: t('plugin_codegen.col.required'),
      align: 'center',
      width: 88,
      render: (c) => {
        if (!value || !c.supported || c.system) return null
        const cur = value.columns.find((x) => x.name === c.name)
        if (!cur) return null
        return (
          <Checkbox
            checked={cur.required}
            disabled={!REQUIRABLE.includes(c.kind) || !cur.form || c.name === value.foreignKey}
            onCheckedChange={(v) => patch(c.name, { required: v === true })}
            aria-label={`${c.name} ${t('plugin_codegen.col.required')}`}
          />
        )
      },
    },
    {
      key: 'control',
      title: t('plugin_codegen.col.control'),
      width: 190,
      render: (c) => {
        if (!value || !c.supported || c.system) return <span className="text-muted-foreground">-</span>
        const cur = value.columns.find((x) => x.name === c.name)
        const options = CONTROLS[c.kind] ?? []
        if (!cur || !cur.form || options.length === 0) return <span className="text-muted-foreground">-</span>
        return (
          <Select value={cur.control || options[0]} onValueChange={(v) => patch(c.name, { control: v as ColumnConfig['control'], dict: v === 'dict' ? cur.dict || '' : undefined })}>
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
        )
      },
    },
  ]

  return (
    <section className="flex flex-col gap-3 rounded-lg border border-border p-3">
      <div className="flex flex-col gap-1">
        <Label>{t('plugin_codegen.sub.title')}</Label>
        <p className="text-xs text-muted-foreground">{t('plugin_codegen.sub.hint')}</p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={table || '__none__'}
          onValueChange={(v) => {
            if (v === '__none__') {
              setTable('')
              setSc(undefined)
              onChange(undefined)
            } else {
              setTable(v)
            }
          }}
        >
          <SelectTrigger className="w-56" aria-label={t('plugin_codegen.sub.select')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__none__">{t('plugin_codegen.sub.none')}</SelectItem>
            {candidates.map((x) => (
              <SelectItem key={x.name} value={x.name}>
                {x.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {value ? (
          <Select value={value.foreignKey || '__none__'} onValueChange={(v) => onChange({ ...value, foreignKey: v === '__none__' ? '' : v })}>
            <SelectTrigger className="w-52" aria-label={t('plugin_codegen.sub.fk')}>
              <SelectValue placeholder={t('plugin_codegen.sub.fk')} />
            </SelectTrigger>
            <SelectContent>
              {fkOptions.map((c) => (
                <SelectItem key={c.name} value={c.name}>
                  {c.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
      </div>
      {value && !value.foreignKey ? <p className="text-xs text-destructive">{t('plugin_codegen.sub.fkRequired')}</p> : null}
      {value && sc ? (
        <DataTable
          columns={columns}
          data={sc.columns}
          rowKey="name"
          pagination={false}
          bare
          emptyText={t('plugin_codegen.col.empty')}
        />
      ) : null}
      {table && sc === undefined && schema.isPending ? <p className="text-xs text-muted-foreground">{t('common.loading')}</p> : null}
    </section>
  )
}
