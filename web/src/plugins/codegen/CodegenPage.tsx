import { useEffect, useState } from 'react'
import { ArrowLeft, ArrowRight, Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import {
  GO_MODULE_RE,
  MODULE_RE,
  useColumns,
  useDownload,
  usePreview,
  useTables,
  type ColumnConfig,
  type GenOutput,
  type GenRequest,
  type TableInfo,
} from './api'
import { ColumnsStep } from './ColumnsStep'
import { PreviewStep } from './PreviewStep'

type Step = 'table' | 'config' | 'preview'

export default function CodegenPage() {
  const { t } = useTranslation()
  const [step, setStep] = useState<Step>('table')
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [table, setTable] = useState<string | undefined>()
  const [module, setModule] = useState('')
  const [goModule, setGoModule] = useState('')
  const [cfg, setCfg] = useState<ColumnConfig[]>([])
  const [output, setOutput] = useState<GenOutput | undefined>()
  const [doneOpen, setDoneOpen] = useState(false)

  const tables = useTables()
  const schema = useColumns(table)
  const preview = usePreview()
  const download = useDownload()

  // 换表或重新读到列：重置为服务端给出的推荐配置，并丢弃旧的预览
  const loaded = schema.data
  useEffect(() => {
    if (!loaded) return
    setCfg(loaded.columns.map((c) => c.config))
    setModule(loaded.suggestedModule)
    setOutput(undefined)
  }, [loaded])

  const moduleError = module && !MODULE_RE.test(module) ? t('plugin_codegen.errors.module') : undefined
  const goModuleError = goModule && !GO_MODULE_RE.test(goModule) ? t('plugin_codegen.errors.goModule') : undefined
  const canPreview = !!table && !!loaded && !!module && !moduleError && !goModuleError

  const request = (): GenRequest => ({ table: table ?? '', module, goModule: goModule || undefined, columns: cfg })

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

  const rows = (tables.data ?? []).filter((x) => x.name.includes(keyword.trim().toLowerCase()))
  const tableColumns: Column<TableInfo>[] = [
    { key: 'name', title: t('plugin_codegen.tables.name'), mobile: 'title', render: (r) => <span className="font-mono text-sm">{r.name}</span> },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_codegen.title')} description={t('plugin_codegen.description')} />
      <Tabs value={step} onValueChange={(v) => setStep(v as Step)} className="flex flex-col gap-4">
        <TabsList>
          <TabsTrigger value="table">{t('plugin_codegen.steps.table')}</TabsTrigger>
          <TabsTrigger value="config" disabled={!table}>
            {t('plugin_codegen.steps.config')}
          </TabsTrigger>
          <TabsTrigger value="preview" disabled={!output}>
            {t('plugin_codegen.steps.preview')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="table" className="m-0 flex flex-col gap-4">
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
            onRowClick={(r) => {
              setTable(r.name)
              setStep('config')
            }}
            rowActions={(r) => (
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setTable(r.name)
                  setStep('config')
                }}
              >
                {t('plugin_codegen.tables.select')}
              </Button>
            )}
          />
        </TabsContent>

        <TabsContent value="config" className="m-0 flex flex-col gap-4">
          {loaded ? (
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
          ) : (
            <p className="text-sm text-muted-foreground">{schema.isError ? t('plugin_codegen.errors.load') : t('common.loading')}</p>
          )}
          <div className="flex flex-wrap justify-end gap-2">
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
            <Can perm="codegen:download">
              <Button onClick={onDownload} disabled={!output || download.isPending}>
                <Download aria-hidden="true" />
                {t('plugin_codegen.action.download')}
              </Button>
            </Can>
          </div>
        </TabsContent>
      </Tabs>

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
