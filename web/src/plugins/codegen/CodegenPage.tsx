import { useEffect, useState } from 'react'
import { ArrowLeft, ArrowRight, Database, Download, FolderInput, Plus, Save, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import {
  GO_MODULE_RE,
  MODULE_RE,
  useColumns,
  useConfigs,
  useDeleteConfig,
  useDeleteConfigs,
  useDownload,
  useInstall,
  usePreview,
  useSaveConfig,
  useSchema,
  useTables,
  type ColumnConfig,
  type GenOutput,
  type GenRequest,
  type SavedGenConfig,
  type SubTableSpec,
  type TableDef,
  type TableInfo,
  type TableSchema,
} from './api'
import { ColumnsStep } from './ColumnsStep'
import { DefineTableDialog } from './DefineTableDialog'
import { PreviewStep } from './PreviewStep'
import { SubStep } from './SubStep'

type Step = 'table' | 'config' | 'preview'
type Source = { kind: 'table'; name: string } | { kind: 'define'; def: TableDef }

export default function CodegenPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [step, setStep] = useState<Step>('table')
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [source, setSource] = useState<Source>()
  const [defined, setDefined] = useState<TableSchema>()
  const [module, setModule] = useState('')
  const [goModule, setGoModule] = useState('')
  const [cfg, setCfg] = useState<ColumnConfig[]>([])
  const [output, setOutput] = useState<GenOutput | undefined>()
  const [doneOpen, setDoneOpen] = useState(false)
  const [defineOpen, setDefineOpen] = useState(false)
  // 载入已保存配置：等 schema 到手后再覆盖推荐配置；nonce 让重复载入同一份也能触发。
  const [pending, setPending] = useState<{ sig: string; cfg: ColumnConfig[]; module?: string; sub?: SubTableSpec; nonce: number }>()
  const [sub, setSub] = useState<SubTableSpec>()
  const [saveOpen, setSaveOpen] = useState(false)
  const [saveName, setSaveName] = useState('')
  const [installOpen, setInstallOpen] = useState(false)
  const [overwrite, setOverwrite] = useState(false)
  const install = useInstall()

  const tables = useTables()
  const schema = useColumns(source?.kind === 'table' ? source.name : undefined)
  const defineSchema = useSchema()
  const preview = usePreview()
  const download = useDownload()
  const configs = useConfigs()
  const saveConfig = useSaveConfig()
  const removeConfig = useDeleteConfig()
  const removeConfigs = useDeleteConfigs()
  // 勾选的是「已保存配置」那张表（可删实体）；上面的表列表只是选表入口，不能删。
  const [selectedCfgs, setSelectedCfgs] = useState<string[]>([])

  const loaded = source?.kind === 'define' ? defined : source?.kind === 'table' ? schema.data : undefined
  const sig = source ? (source.kind === 'define' ? `define:${source.def.name}` : `table:${source.name}`) : ''

  // 换表或重新读到列：默认重置为服务端推荐配置；载入中的保存配置优先，并丢弃旧的预览。
  useEffect(() => {
    if (!loaded) return
    if (pending && pending.sig === sig) {
      setCfg(pending.cfg)
      if (pending.module) setModule(pending.module)
      setSub(pending.sub)
      setPending(undefined)
      setOutput(undefined)
      return
    }
    setCfg(loaded.columns.map((c) => c.config))
    setModule(loaded.suggestedModule)
    setSub(undefined)
    setOutput(undefined)
  }, [loaded, pending, sig])

  const moduleError = module && !MODULE_RE.test(module) ? t('plugin_codegen.errors.module') : undefined
  const goModuleError = goModule && !GO_MODULE_RE.test(goModule) ? t('plugin_codegen.errors.goModule') : undefined
  const canPreview = !!source && !!loaded && !!module && !moduleError && !goModuleError

  const request = (): GenRequest => {
    const base = { module, goModule: goModule || undefined, columns: cfg, sub: sub?.foreignKey ? sub : undefined }
    return source?.kind === 'define'
      ? { ...base, define: source.def }
      : { ...base, table: source?.kind === 'table' ? source.name : '' }
  }

  const onPreview = async () => {
    try {
      setOutput(await preview.mutateAsync(request()))
      setStep('preview')
    } catch {
      // 错误提示由请求层统一弹出
    }
  }

  const onDownload = async () => {
    try {
      await download.mutateAsync(request())
      toast.success(t('plugin_codegen.downloaded'))
      setDoneOpen(true)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onInstall = async () => {
    try {
      const res = await install.mutateAsync({ ...request(), overwrite })
      setInstallOpen(false)
      toast.success(
        t('plugin_codegen.install.done', { written: res.written.length, skipped: res.skipped.length }),
      )
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onDefine = async (def: TableDef) => {
    try {
      const sc = await defineSchema.mutateAsync({ define: def })
      setSource({ kind: 'define', def })
      setDefined(sc)
      setDefineOpen(false)
      setStep('config')
    } catch {
      // 错误提示由请求层统一弹出
    }
  }

  const pickTable = (name: string) => {
    setSource({ kind: 'table', name })
    setStep('config')
  }

  // 载入已保存配置：先补齐 schema（define 要重新合成一次），schema 到手后 effect 里覆盖配置。
  const loadConfig = async (c: SavedGenConfig) => {
    try {
      if (c.define) {
        const sc = await defineSchema.mutateAsync({ define: c.define })
        setDefined(sc)
        setSource({ kind: 'define', def: c.define })
      } else {
        setSource({ kind: 'table', name: c.table })
      }
      setPending({
        sig: c.define ? `define:${c.define.name}` : `table:${c.table}`,
        cfg: c.columns ?? [],
        module: c.module,
        sub: c.sub,
        nonce: Date.now(),
      })
      setStep('config')
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onSave = async () => {
    const name = saveName.trim()
    if (!name) return
    try {
      await saveConfig.mutateAsync({
        name,
        module,
        goModule: goModule || undefined,
        table: source?.kind === 'table' ? source.name : undefined,
        define: source?.kind === 'define' ? source.def : undefined,
        columns: cfg,
        sub: sub?.foreignKey ? sub : undefined,
      })
      setSaveOpen(false)
      toast.success(t('plugin_codegen.saved.saved', { name }))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onDeleteConfig = async (c: SavedGenConfig) => {
    if (await confirm({ destructive: true, description: t('plugin_codegen.saved.deleteConfirm', { name: c.name }) })) {
      try {
        await removeConfig.mutateAsync(c.id)
        setSelectedCfgs((s) => s.filter((id) => id !== c.id))
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  // 批量删：一次提交点名的 id。后端一条 DELETE 只动这些 id，deleted 是实删条数
  // （已被别人删掉的不计入，一条都没删到时报 404 走失败分支），据实提示不写成「全部删除成功」。
  const onDeleteConfigBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('plugin_codegen.saved.deleteBatchConfirm', { count: ids.length }) }))) {
      return
    }
    try {
      const { deleted } = await removeConfigs.mutateAsync(ids)
      setSelectedCfgs([])
      toast.success(deleted < ids.length ? t('plugin_codegen.saved.deletedPartial', { deleted, total: ids.length }) : t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const allTables = tables.data ?? []
  const rows = allTables.filter((x) => x.name.includes(keyword.trim().toLowerCase()))
  const tableColumns: Column<TableInfo>[] = [
    { key: 'name', title: t('plugin_codegen.tables.name'), mobile: 'title', render: (r) => <span className="font-mono text-sm">{r.name}</span> },
  ]
  const savedConfigs = configs.data ?? []
  const configColumns: Column<SavedGenConfig>[] = [
    { key: 'name', title: t('plugin_codegen.saved.name'), mobile: 'title', render: (r) => <span className="font-medium">{r.name}</span> },
    { key: 'module', title: t('plugin_codegen.config.module'), render: (r) => <Badge variant="outline">{r.module}</Badge> },
    {
      key: 'table',
      title: t('plugin_codegen.tables.name'),
      render: (r) => <span className="font-mono text-xs text-muted-foreground">{r.define ? `biz_${r.define.name}` : r.table}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_codegen.title')} description={t('plugin_codegen.description')} />
      <Tabs value={step} onValueChange={(v) => setStep(v as Step)} className="flex flex-col gap-4">
        <TabsList>
          <TabsTrigger value="table">{t('plugin_codegen.steps.table')}</TabsTrigger>
          <TabsTrigger value="config" disabled={!source}>
            {t('plugin_codegen.steps.config')}
          </TabsTrigger>
          <TabsTrigger value="preview" disabled={!output}>
            {t('plugin_codegen.steps.preview')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="table" className="m-0 flex flex-col gap-4">
          {tables.isError ? (
            <EmptyState
              icon={<Database aria-hidden="true" />}
              title={t('plugin_codegen.tables.loadFailed')}
              description={t('plugin_codegen.tables.loadFailedHint')}
              action={
                <Button variant="outline" onClick={() => void tables.refetch()}>
                  {t('plugin_codegen.tables.retry')}
                </Button>
              }
            />
          ) : !tables.isLoading && allTables.length === 0 ? (
            <EmptyState
              icon={<Database aria-hidden="true" />}
              title={t('plugin_codegen.tables.emptyTitle')}
              description={t('plugin_codegen.tables.emptyHint')}
              action={
                <div className="flex flex-wrap justify-center gap-2">
                  <Button onClick={() => setDefineOpen(true)}>
                    <Plus aria-hidden="true" />
                    {t('plugin_codegen.tables.new')}
                  </Button>
                  <Button variant="outline" onClick={() => void tables.refetch()}>
                    {t('plugin_codegen.tables.refresh')}
                  </Button>
                </div>
              }
            />
          ) : (
            <>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm text-muted-foreground">{t('plugin_codegen.tables.hint')}</p>
                <Button onClick={() => setDefineOpen(true)}>
                  <Plus aria-hidden="true" />
                  {t('plugin_codegen.tables.new')}
                </Button>
              </div>
              <FilterBar
                onSearch={() => setKeyword(draft)}
                onReset={() => {
                  setDraft('')
                  setKeyword('')
                }}
              >
                <Input
                  className="md:w-56"
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  placeholder={t('plugin_codegen.tables.search')}
                  aria-label={t('plugin_codegen.tables.search')}
                />
              </FilterBar>
              <DataTable
                columns={tableColumns}
                data={rows}
                rowKey="name"
                loading={tables.isLoading}
                pagination={false}
                emptyText={t('plugin_codegen.tables.empty')}
                onRowClick={(r) => pickTable(r.name)}
                rowActions={(r) => (
                  <Button variant="outline" size="sm" onClick={() => pickTable(r.name)}>
                    {t('plugin_codegen.tables.select')}
                  </Button>
                )}
              />
              {savedConfigs.length > 0 ? (
                <section className="flex flex-col gap-2">
                  <p className="text-sm font-medium text-muted-foreground">{t('plugin_codegen.saved.title')}</p>
                  <DataTable
                    columns={configColumns}
                    data={savedConfigs}
                    rowKey="id"
                    pagination={false}
                    emptyText={t('plugin_codegen.saved.empty')}
                    selection={{ selected: selectedCfgs, onChange: setSelectedCfgs }}
                    batchActions={(ids) => (
                      <Can perm="codegen:edit">
                        <Button variant="destructive" size="sm" disabled={removeConfigs.isPending} onClick={() => void onDeleteConfigBatch(ids)}>
                          <Trash2 aria-hidden="true" />
                          {t('common.delete')}
                        </Button>
                      </Can>
                    )}
                    onRowClick={(r) => void loadConfig(r)}
                    rowActions={(r) => (
                      <>
                        <Button variant="outline" size="sm" onClick={() => void loadConfig(r)}>
                          {t('plugin_codegen.saved.load')}
                        </Button>
                        <Can perm="codegen:edit">
                          <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDeleteConfig(r)}>
                            <Trash2 aria-hidden="true" />
                          </Button>
                        </Can>
                      </>
                    )}
                  />
                </section>
              ) : null}
            </>
          )}
        </TabsContent>

        <TabsContent value="config" className="m-0 flex flex-col gap-4">
          {loaded ? (
            <div className="flex flex-col gap-3">
              {source?.kind === 'define' ? (
                <p className="text-xs text-muted-foreground">{t('plugin_codegen.define.pendingHint', { table: loaded.table })}</p>
              ) : null}
              <ColumnsStep
                schema={loaded}
                cfg={cfg}
                onCfgChange={setCfg}
                module={module}
                onModuleChange={setModule}
                goModule={goModule}
                onGoModuleChange={setGoModule}
                moduleError={moduleError}
                goModuleError={goModuleError}
              />
              {source?.kind === 'table' ? (
                <SubStep tables={allTables} mainTable={source.name} value={sub} onChange={(v) => { setSub(v); setOutput(undefined) }} />
              ) : null}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">{schema.isError ? t('plugin_codegen.errors.load') : t('common.loading')}</p>
          )}
          <div className="flex flex-wrap justify-end gap-2">
            <Can perm="codegen:edit">
              <Button variant="outline" onClick={() => { setSaveName(module); setSaveOpen(true) }} disabled={!canPreview}>
                <Save className="me-2 size-4" aria-hidden="true" />
                {t('plugin_codegen.action.saveConfig')}
              </Button>
            </Can>
            <Button variant="outline" onClick={() => setStep('table')}>
              <ArrowLeft className="rtl:rotate-180" aria-hidden="true" />
              {t('plugin_codegen.action.back')}
            </Button>
            <Button onClick={onPreview} disabled={!canPreview || preview.isPending}>
              {t('plugin_codegen.action.preview')}
              <ArrowRight className="rtl:rotate-180" aria-hidden="true" />
            </Button>
          </div>
        </TabsContent>

        <TabsContent value="preview" className="m-0 flex flex-col gap-4">
          {output ? <PreviewStep output={output} /> : null}
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="outline" onClick={() => setStep('config')}>
              <ArrowLeft className="rtl:rotate-180" aria-hidden="true" />
              {t('plugin_codegen.action.back')}
            </Button>
            <Can perm="codegen:install">
              <Button variant="outline" onClick={() => { setOverwrite(false); setInstallOpen(true) }} disabled={!output}>
                <FolderInput className="me-2 size-4" aria-hidden="true" />
                {t('plugin_codegen.action.install')}
              </Button>
            </Can>
            <Can perm="codegen:download">
              <Button onClick={onDownload} disabled={!output || download.isPending}>
                <Download aria-hidden="true" />
                {t('plugin_codegen.action.download')}
              </Button>
            </Can>
          </div>
        </TabsContent>
      </Tabs>

      <DefineTableDialog open={defineOpen} onOpenChange={setDefineOpen} pending={defineSchema.isPending} onSubmit={onDefine} />

      <ResponsiveDialog
        open={saveOpen}
        onOpenChange={setSaveOpen}
        title={t('plugin_codegen.saved.saveTitle')}
        footer={
          <>
            <Button variant="outline" onClick={() => setSaveOpen(false)}>{t('common.cancel')}</Button>
            <Button onClick={() => void onSave()} disabled={!saveName.trim() || saveConfig.isPending}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="codegen-save-name">
            {t('plugin_codegen.saved.name')}
            <RequiredMark />
          </Label>
          <Input
            id="codegen-save-name"
            value={saveName}
            onChange={(e) => setSaveName(e.target.value)}
            maxLength={64}
            autoFocus
            onKeyDown={(e) => {
              if (e.key === 'Enter') void onSave()
            }}
          />
          <p className="text-xs text-muted-foreground">{t('plugin_codegen.saved.saveHint')}</p>
        </div>
      </ResponsiveDialog>

      <ResponsiveDialog
        open={installOpen}
        onOpenChange={setInstallOpen}
        title={t('plugin_codegen.install.title')}
        footer={
          <>
            <Button variant="outline" onClick={() => setInstallOpen(false)}>{t('common.cancel')}</Button>
            <Button onClick={() => void onInstall()} disabled={install.isPending}>
              {t('plugin_codegen.action.install')}
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-3 text-sm">
          <p className="text-muted-foreground">{t('plugin_codegen.install.desc')}</p>
          <label className="flex items-center gap-2">
            <Checkbox checked={overwrite} onCheckedChange={(v) => setOverwrite(v === true)} />
            {t('plugin_codegen.install.overwrite')}
          </label>
          <p className="text-xs text-muted-foreground">{t('plugin_codegen.install.overwriteHint')}</p>
          <p className="text-xs text-muted-foreground">{t('plugin_codegen.install.restartHint')}</p>
        </div>
      </ResponsiveDialog>

      <ResponsiveDialog
        open={doneOpen}
        onOpenChange={setDoneOpen}
        title={t('plugin_codegen.done.title')}
        description={t('plugin_codegen.done.description')}
        footer={<Button onClick={() => setDoneOpen(false)}>{t('common.confirm')}</Button>}
      >
        <ul className="flex flex-col gap-2 text-sm">
          {(output?.notes ?? []).map((n) => (
            <li key={n.code}>
              {t(`plugin_codegen.notes.${n.code}`)}
              {n.value ? <code className="ms-2 rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{n.value}</code> : null}
            </li>
          ))}
        </ul>
      </ResponsiveDialog>
    </PageContainer>
  )
}
