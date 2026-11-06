import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowRightLeft, BellRing, CircleCheck, GitBranch, History, Pencil, Plus, Trash2, Undo2, UserPlus, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useAuth } from '@/framework/auth/useAuth'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { StatCard } from '@/framework/components/StatCard'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { resolveUserLabel } from '@/framework/modules/user/api'
import { cn } from '@/lib/utils'
import {
  fetchDefVersionXML,
  useDefs,
  useDefVersions,
  useDeleteDef,
  useDeleteDefs,
  useInstance,
  useInstances,
  useRollbackDef,
  useSaveDef,
  useStartInstance,
  useTemplates,
  useUrgeInstance,
  useWfAction,
  useWorkflowStats,
  type WfDef,
} from './api'
import { formatTime, statusVariant } from './format'
import { ApproverPicker } from './ApproverPicker'
import { InstanceFlow } from './InstanceFlow'
import { StartDialog } from './StartDialog'
import { BLANK_BPMN_XML } from './blank-bpmn'

// bpmn-js 体积较大，只在打开定义编辑器时加载，避免拖慢流程中心首屏。
const BpmnDesigner = lazy(() => import('./BpmnDesigner').then((m) => ({ default: m.BpmnDesigner })))

// WorkflowPage 是工作流中心：待办 / 已办 / 我发起的 / 全部实例 / 流程定义。
// 流程定义用 bpmn-js 标准建模器编辑；实例详情展示只读流程图与当前待办。

type Filter = 'todo' | 'done' | 'mine' | 'all'

export default function WorkflowPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<Filter | 'defs'>('todo')
  const [selected, setSelected] = useState<string | undefined>()
  const [starting, setStarting] = useState(false)
  const { data: stats } = useWorkflowStats()

  const cards = [
    { label: t('plugin_workflow.tabs.todo'), value: stats?.todo ?? 0, tone: 'warning' as const },
    { label: t('plugin_workflow.status.running'), value: stats?.running ?? 0, tone: 'primary' as const },
    { label: t('plugin_workflow.status.approved'), value: stats?.approved ?? 0, tone: 'success' as const },
    { label: t('plugin_workflow.status.rejected'), value: stats?.rejected ?? 0, tone: 'destructive' as const },
    { label: t('plugin_workflow.status.cancelled'), value: stats?.cancelled ?? 0, tone: 'info' as const },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_workflow.title')} description={t('plugin_workflow.description')} />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
        {cards.map((c) => (
          <StatCard key={c.label} title={c.label} value={c.value} tone={c.tone} />
        ))}
      </div>
      <Tabs value={tab} onValueChange={(v) => setTab(v as Filter | 'defs')} className="flex flex-col gap-4">
        {/* 发起入口跟着列表走（与各页 DataTable toolbar 的「新建」同位），不飘在页头右上角 */}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <TabsList>
            <TabsTrigger value="todo">{t('plugin_workflow.tabs.todo')}</TabsTrigger>
            <TabsTrigger value="done">{t('plugin_workflow.tabs.done')}</TabsTrigger>
            <TabsTrigger value="mine">{t('plugin_workflow.tabs.mine')}</TabsTrigger>
            <TabsTrigger value="all">{t('plugin_workflow.tabs.all')}</TabsTrigger>
            <Can perm="workflow:edit">
              <TabsTrigger value="defs">{t('plugin_workflow.tabs.defs')}</TabsTrigger>
            </Can>
          </TabsList>
          <Can perm="workflow:start">
            <Button onClick={() => setStarting(true)}>
              <Plus aria-hidden="true" />
              {t('plugin_workflow.startAction')}
            </Button>
          </Can>
        </div>
        <TabsContent value="todo" className="m-0">
          <InstanceList filter="todo" onSelect={setSelected} />
        </TabsContent>
        <TabsContent value="done" className="m-0">
          <InstanceList filter="done" onSelect={setSelected} />
        </TabsContent>
        <TabsContent value="mine" className="m-0">
          <InstanceList filter="mine" onSelect={setSelected} />
        </TabsContent>
        <TabsContent value="all" className="m-0">
          <InstanceList filter="all" onSelect={setSelected} />
        </TabsContent>
        <Can perm="workflow:edit">
          <TabsContent value="defs" className="m-0">
            <DefList />
          </TabsContent>
        </Can>
      </Tabs>
      <InstanceDrawer id={selected} onClose={() => setSelected(undefined)} />
      <StartFlow open={starting} onClose={() => setStarting(false)} />
    </PageContainer>
  )
}

// ---- 发起 ----

function StartFlow({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const defs = useDefs({ page: 1, pageSize: 200 })
  const start = useStartInstance()

  return (
    <StartDialog
      open={open}
      defs={(defs.data?.list ?? []).map((d) => ({ id: d.id, name: d.name, key: d.key, status: d.status }))}
      onClose={onClose}
      onSubmit={async (v) => {
        try {
          const inst = await start.mutateAsync({
            defId: v.defId, title: v.title, bizType: v.bizType, businessKey: v.businessKey, vars: v.vars ?? {},
          })
          toast.success(t('plugin_workflow.startSuccess', { title: inst.title || v.title }))
          onClose()
        } catch {
          toast.error(t('common.operateFailed'))
        }
      }}
    />
  )
}

// ---- 实例列表 ----

function InstanceList({ filter, onSelect }: { filter: Filter; onSelect: (id: string) => void }) {
  const { t, i18n } = useTranslation()
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)
  const [keyword, setKeyword] = useState('')
  const [applied, setApplied] = useState('')
  const [status, setStatus] = useState('')
  // Radix Select 的选项值不能是空串：空值触发器渲染成空白，「全部」用一个非空哨兵值表达。
  const ALL = '__all'

  useEffect(() => {
    const timer = setTimeout(() => {
      setApplied(keyword.trim())
      setPage(1)
    }, 300)
    return () => clearTimeout(timer)
  }, [keyword])

  const query = {
    page,
    pageSize,
    keyword: applied || undefined,
    status: status || undefined,
    todo: filter === 'todo' ? '1' : undefined,
    done: filter === 'done' ? '1' : undefined,
    mine: filter === 'mine' ? 'started' : undefined,
  }
  const instances = useInstances(query)
  const names = useUserNames((instances.data?.list ?? []).map((w) => w.submitter))
  const rows = instances.data?.list ?? []
  // 关键词四个实例 tab 都给（待办多了找单子靠它）；状态筛选只对完整列表有意义。
  const showStatus = filter === 'all' || filter === 'mine'

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
          <Input
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder={t('plugin_workflow.searchPlaceholder')}
            aria-label={t('plugin_workflow.searchPlaceholder')}
            className="w-full sm:w-64"
          />
          {showStatus ? (
            <SimpleSelect
              className="w-36"
              aria-label={t('plugin_workflow.fields.status')}
              value={status || ALL}
              onChange={(v) => {
                setStatus(v === ALL ? '' : v)
                setPage(1)
              }}
              options={[
                { value: ALL, label: t('plugin_workflow.statusAll') },
                { value: 'running', label: t('plugin_workflow.status.running') },
                { value: 'approved', label: t('plugin_workflow.status.approved') },
                { value: 'rejected', label: t('plugin_workflow.status.rejected') },
                { value: 'cancelled', label: t('plugin_workflow.status.cancelled') },
              ]}
            />
          ) : null}
      </div>

      <div className="overflow-hidden rounded-lg border bg-card">
        {instances.isLoading && rows.length === 0 ? (
          <div className="p-4 text-center text-sm text-muted-foreground">{t('plugin_workflow.loading')}</div>
        ) : null}
        {rows.map((w) => (
          <button
            key={w.id}
            type="button"
            onClick={() => onSelect(w.id)}
            className={cn('flex w-full flex-col gap-1 border-b px-4 py-2.5 text-start text-sm last:border-b-0 hover:bg-accent')}
          >
            <div className="flex w-full items-center gap-2">
              <span className="min-w-0 flex-1 truncate font-medium text-foreground">{w.title || w.defKey}</span>
              <Badge variant={statusVariant(w.status)}>{t(`plugin_workflow.status.${w.status}`)}</Badge>
            </div>
            <div className="flex w-full items-center gap-2 text-xs text-muted-foreground">
              <span className="min-w-0 flex-1 truncate">
                {w.businessKey ? `${w.bizType}/${w.businessKey} · ` : ''}
                {w.pending.length > 0
                  ? `${t('plugin_workflow.currentApprovers')}: ${[
                      ...new Set(w.pending.map((p) => (p.assignee ? names(p.assignee) || p.nodeName : t('plugin_workflow.anyone')))),
                    ].join('、')}`
                  : w.current.join(', ')}
              </span>
              <span>{t('plugin_workflow.submitter')}: {names(w.submitter) || t('plugin_workflow.unknownUser')}</span>
              <span>{formatTime(w.updatedAt, i18n.language)}</span>
            </div>
          </button>
        ))}
        {!instances.isLoading && rows.length === 0 ? (
          <EmptyState title={t('plugin_workflow.empty')} className="py-8" />
        ) : null}
        {(instances.data?.total ?? 0) > page * pageSize || page > 1 ? (
          <div className="flex items-center justify-between border-t px-4 py-2 text-xs text-muted-foreground">
            <span>{t('plugin_workflow.total', { count: instances.data?.total ?? 0 })}</span>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                {t('plugin_workflow.prev')}
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={page * pageSize >= (instances.data?.total ?? 0)}
                onClick={() => setPage((p) => p + 1)}
              >
                {t('plugin_workflow.next')}
              </Button>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  )
}

// ---- 用户展示名（审批人/提交人不上屏原始 ID） ----

function useUserNames(ids: string[]) {
  const [names, setNames] = useState<Record<string, string>>({})
  const key = useMemo(() => [...new Set(ids.filter(Boolean))].sort().join(','), [ids])

  useEffect(() => {
    if (!key) return
    const ac = new AbortController()
    let alive = true
    void Promise.all(
      key.split(',').map(async (id) => [id, await resolveUserLabel(id, ac.signal)] as const),
    ).then((entries) => {
      if (!alive) return
      const next: Record<string, string> = {}
      for (const [id, name] of entries) if (name) next[id] = name
      setNames((prev) => ({ ...prev, ...next }))
    })
    return () => {
      alive = false
      ac.abort()
    }
  }, [key])

  // 解析不到时返回空串：上屏绝不回退到原始用户 ID。
  return (id: string) => (id ? names[id] || '' : '')
}

// ---- 流程定义（bpmn-js 建模） ----

function DefList() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [editing, setEditing] = useState<WfDef | 'new' | null>(null)
  const [versionsOf, setVersionsOf] = useState<WfDef | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(50)
  // 勾选用独立状态：外层 selected 是「打开哪个实例详情」，与批量选择不是一回事。
  const [selectedDefs, setSelectedDefs] = useState<string[]>([])

  const defs = useDefs({ page, pageSize })
  const remove = useDeleteDef()
  const removeBatch = useDeleteDefs()
  const templates = useTemplates()

  const onDelete = async (d: WfDef) => {
    if (await confirm({ destructive: true, description: t('plugin_workflow.defDeleteConfirm', { name: d.name }) })) {
      try {
        await remove.mutateAsync(d.id)
        setSelectedDefs((s) => s.filter((id) => id !== d.id))
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  // 批量删：一次提交点名的 id，后端逐条走单条删除的判断并在一个事务里走完。
  // 在途实例按发起时存下的定义快照继续执行、历史版本与待办都不随定义删除（与单条一致），
  // deleted 是实删条数：越权/已被删掉的 id 不计入，一条都没删到时整批报 404 走失败分支。
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('plugin_workflow.defDeleteBatchConfirm', { count: ids.length }) }))) {
      return
    }
    try {
      const { deleted } = await removeBatch.mutateAsync(ids)
      setSelectedDefs([])
      toast.success(deleted < ids.length ? t('plugin_workflow.deletedPartial', { deleted, total: ids.length }) : t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<WfDef>[] = [
    {
      key: 'name',
      title: t('plugin_workflow.fields.defName'),
      mobile: 'title',
      render: (d) => (
        <div>
          <span className="font-medium text-foreground">{d.name}</span>
          <div className="text-xs text-muted-foreground">
            {d.key} · v{d.version}
          </div>
        </div>
      ),
    },
    {
      key: 'status',
      title: t('plugin_workflow.fields.status'),
      render: (d) => (
        <Badge variant={d.status === 'published' ? 'success' : d.status === 'disabled' ? 'destructive' : 'secondary'}>
          {t(`plugin_workflow.defStatus.${d.status}`)}
        </Badge>
      ),
    },
    {
      key: 'updatedAt',
      title: t('plugin_workflow.fields.updatedAt'),
      render: (d) => <span className="num text-xs text-muted-foreground">{formatTime(d.updatedAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="space-y-4">
      <DataTable
        columns={columns}
        data={defs.data?.list ?? []}
        rowKey="id"
        loading={defs.isLoading}
        columnSettingsKey="plugin-workflow-defs"
        pagination={{ page, pageSize, total: defs.data?.total ?? 0, onChange: setPage }}
        emptyText={t('plugin_workflow.empty')}
        selection={{ selected: selectedDefs, onChange: setSelectedDefs }}
        batchActions={(ids) => (
          <Can perm="workflow:edit">
            <Button variant="destructive" size="sm" disabled={removeBatch.isPending} onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        toolbar={
          <Can perm="workflow:edit">
            <Button onClick={() => setEditing('new')}>
              <Plus aria-hidden="true" />
              {t('plugin_workflow.defCreate')}
            </Button>
          </Can>
        }
        rowActions={(d) => (
          <Can perm="workflow:edit">
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => setEditing(d)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('plugin_workflow.defVersions')} onClick={() => setVersionsOf(d)}>
              <History aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(d)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />

      {editing ? (
        <DefEditor
          def={editing === 'new' ? null : editing}
          templates={templates.data ?? []}
          onClose={() => setEditing(null)}
        />
      ) : null}
      <DefVersionsDialog def={versionsOf} onClose={() => setVersionsOf(null)} />
    </div>
  )
}

// ---- 定义版本历史 ----

function DefVersionsDialog({ def, onClose }: { def: WfDef | null; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const rollback = useRollbackDef()
  const { data: versions = [], isLoading } = useDefVersions(def?.id)
  const [xml, setXml] = useState<{ version: number; text: string } | null>(null)
  const [loadingXml, setLoadingXml] = useState(false)

  const onView = async (version: number) => {
    if (xml?.version === version) {
      setXml(null)
      return
    }
    setLoadingXml(true)
    try {
      setXml({ version, text: await fetchDefVersionXML(def!.id, version) })
    } catch {
      toast.error(t('common.operateFailed'))
    } finally {
      setLoadingXml(false)
    }
  }

  const onRollback = async (version: number) => {
    if (!def || !(await confirm({ destructive: true, description: t('plugin_workflow.rollbackVersionConfirm', { version }) }))) return
    try {
      await rollback.mutateAsync({ id: def.id, version })
      toast.success(t('plugin_workflow.rollbackVersionDone', { version }))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={!!def}
      onOpenChange={(o) => !o && onClose()}
      title={`${t('plugin_workflow.defVersions')} · ${def?.name ?? ''}`}
      description={t('plugin_workflow.defVersionsHint', { version: def?.version ?? 0 })}
      size="lg"
      footer={
        <Button variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {isLoading ? <p className="text-sm text-muted-foreground">{t('plugin_workflow.loading')}</p> : null}
      {!isLoading && versions.length === 0 ? (
        <EmptyState title={t('plugin_workflow.defVersionsEmpty')} />
      ) : null}
      <ul className="flex flex-col divide-y">
        {versions.map((v) => (
          <li key={v.version} className="flex flex-wrap items-center gap-2 py-2 text-sm">
            <span className="num w-12 font-medium">v{v.version}</span>
            <span className="min-w-0 flex-1 truncate text-muted-foreground">{v.name}</span>
            <span className="num text-xs text-muted-foreground">{formatTime(v.updatedAt, i18n.language)}</span>
            <Button variant="ghost" size="sm" onClick={() => void onView(v.version)}>
              {xml?.version === v.version ? t('plugin_workflow.hideXml') : t('plugin_workflow.viewXml')}
            </Button>
            <Button variant="outline" size="sm" disabled={rollback.isPending} onClick={() => void onRollback(v.version)}>
              {t('plugin_workflow.rollbackVersion')}
            </Button>
          </li>
        ))}
      </ul>
      {loadingXml ? <p className="text-xs text-muted-foreground">{t('plugin_workflow.loading')}</p> : null}
      {xml ? (
        <pre className="max-h-72 overflow-auto rounded-md border border-border bg-muted/40 p-3 text-xs">{xml.text}</pre>
      ) : null}
    </ResponsiveDialog>
  )
}

function DefEditor({
  def,
  templates,
  onClose,
}: {
  def: WfDef | null
  templates: { key: string; name: string; definition: { key: string }; xml?: string }[]
  onClose: () => void
}) {
  const { t } = useTranslation()
  const save = useSaveDef()
  const [name, setName] = useState(def?.name ?? '')
  const [key, setKey] = useState(def?.key ?? '')
  const [status, setStatus] = useState(def?.status ?? 'draft')
  const [templateKey, setTemplateKey] = useState('__blank')
  const [xml, setXml] = useState(def?.xml || BLANK_BPMN_XML)

  const submit = async () => {
    if (!name.trim() || !key.trim()) {
      toast.error(t('plugin_workflow.defNameKeyRequired'))
      return
    }
    if (!xml.trim()) {
      toast.error(t('plugin_workflow.designer.emptyDiagram'))
      return
    }
    try {
      await save.mutateAsync({
        id: def?.id,
        data: {
          key: key.trim(),
          name: name.trim(),
          status,
          // 定义以 XML 为准，由后端 ImportXML 解析；definition 仅作兼容字段。
          definition: def?.definition ?? { key: key.trim(), name: name.trim(), start: 'start', nodes: [], edges: [] },
          xml,
        },
      })
      toast.success(t('common.operateSuccess'))
      onClose()
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={(v) => !v && onClose()}
      title={def ? t('plugin_workflow.defEdit') : t('plugin_workflow.defCreate')}
      size="xl"
      className="space-y-4"
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={save.isPending}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="mb-3 flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs text-muted-foreground">
            {t('plugin_workflow.fields.defName')}
            <span aria-hidden="true" className="ms-0.5 text-destructive">*</span>
          </span>
          <Input required value={name} onChange={(e) => setName(e.target.value)} className="w-56" />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs text-muted-foreground">
            {t('plugin_workflow.fields.defKey')}
            <span aria-hidden="true" className="ms-0.5 text-destructive">*</span>
          </span>
          <Input required value={key} onChange={(e) => setKey(e.target.value)} disabled={!!def} className="w-44 font-mono" />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs text-muted-foreground">{t('plugin_workflow.fields.status')}</span>
          <SimpleSelect
            className="w-32"
            aria-label={t('plugin_workflow.fields.status')}
            value={status}
            onChange={(v) => setStatus(v as 'draft' | 'published' | 'disabled')}
            options={[
              { value: 'draft', label: t('plugin_workflow.defStatus.draft') },
              { value: 'published', label: t('plugin_workflow.defStatus.published') },
              { value: 'disabled', label: t('plugin_workflow.defStatus.disabled') },
            ]}
          />
        </label>
        {!def && templates.length > 0 ? (
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-xs text-muted-foreground">{t('plugin_workflow.fromTemplate')}</span>
            <SimpleSelect
              className="w-44"
              aria-label={t('plugin_workflow.fromTemplate')}
              value={templateKey}
              onChange={(v) => {
                setTemplateKey(v)
                const tpl = templates.find((tp) => tp.key === v)
                if (tpl) {
                  setKey(tpl.definition.key)
                  setName(tpl.name)
                  if (tpl.xml) setXml(tpl.xml)
                }
              }}
              options={[
                { value: '__blank', label: t('plugin_workflow.fromTemplateBlank') },
                ...templates.map((tp) => ({ value: tp.key, label: tp.name })),
              ]}
            />
          </label>
        ) : null}
      </div>
      <Suspense
        fallback={
          <div className="flex h-[360px] items-center justify-center rounded-lg border bg-muted/20 text-sm text-muted-foreground md:h-[480px]">
            {t('plugin_workflow.loading')}
          </div>
        }
      >
        <BpmnDesigner xml={xml} onChange={setXml} />
      </Suspense>
    </ResponsiveDialog>
  )
}

// ---- 实例详情抽屉 ----

function InstanceDrawer({ id, onClose }: { id?: string; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const user = useAuth((s) => s.user)
  // Radix Select 选项值不能为空串：回退目标下拉的「未选择」用非空哨兵值。
  const NONE = '__none'
  const [comment, setComment] = useState('')
  const [countersignUsers, setCountersignUsers] = useState<string[]>([])
  const [countersignOpen, setCountersignOpen] = useState(false)
  const [transferTo, setTransferTo] = useState<string[]>([])
  const [transferOpen, setTransferOpen] = useState(false)
  const [rollbackTo, setRollbackTo] = useState('')
  const detail = useInstance(id)
  const action = useWfAction()
  const urge = useUrgeInstance()
  const inst = detail.data

  const names = useUserNames([inst?.submitter ?? '', ...(inst?.history.map((h) => h.actor) ?? [])])
  const approvalNodes = useMemo(
    () => (inst?.definition?.nodes ?? []).filter((n) => n.kind === 'approval'),
    [inst?.definition],
  )
  const myTask = inst?.pending?.some((p) => !p.assignee || p.assignee === user?.id) ?? false
  const canWithdraw = inst?.status === 'running' && !!user?.id && user.id === inst.submitter

  const onUrge = async () => {
    if (!inst) return
    try {
      await urge.mutateAsync(inst.id)
      toast.success(t('plugin_workflow.urgeDone'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  useEffect(() => {
    setComment('')
    setCountersignUsers([])
    setCountersignOpen(false)
    setTransferTo([])
    setTransferOpen(false)
    setRollbackTo('')
  }, [id])

  // 抽屉是手写 overlay，没有 Radix 的焦点管理：Esc 关闭 + Tab 焦点圈定（首尾循环），
  // 键盘用户才不会被锁在页面里，也不会 Tab 到抽屉背后的元素上。
  const drawerRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!id) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose()
        return
      }
      if (e.key !== 'Tab' || !drawerRef.current) return
      const focusables = drawerRef.current.querySelectorAll<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      )
      if (focusables.length === 0) return
      const first = focusables[0]
      const last = focusables[focusables.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [id, onClose])

  if (!id) return null

  const isRunning = inst?.status === 'running'

  const doAction = async (act: 'approve' | 'reject' | 'escalate' | 'withdraw' | 'rollback', confirmMsg?: string) => {
    if (!inst) return
    if (confirmMsg && !(await confirm({ destructive: act === 'withdraw' || act === 'reject', description: confirmMsg }))) return
    try {
      await action.mutateAsync({
        id: inst.id,
        action: act,
        body: act === 'rollback' ? { comment: comment.trim(), toNode: rollbackTo } : { comment: comment.trim() },
      })
      toast.success(t('common.operateSuccess'))
      setComment('')
      setRollbackTo('')
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const doCountersign = async () => {
    if (!inst || countersignUsers.length === 0) return
    try {
      for (const approver of countersignUsers) {
        await action.mutateAsync({ id: inst.id, action: 'countersign', body: { approver, comment: comment.trim() } })
      }
      toast.success(t('plugin_workflow.countersignDone', { count: countersignUsers.length }))
      setCountersignUsers([])
      setCountersignOpen(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const doTransfer = async () => {
    if (!inst || transferTo.length === 0) return
    try {
      await action.mutateAsync({ id: inst.id, action: 'transfer', body: { to: transferTo[0], comment: comment.trim() } })
      toast.success(t('plugin_workflow.transferDone'))
      setTransferTo([])
      setTransferOpen(false)
      setComment('')
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-black/40" onClick={onClose}>
      <div
        ref={drawerRef}
        className="flex h-full w-full max-w-2xl flex-col border-l bg-card shadow-xl"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label={inst?.title || t('plugin_workflow.title')}
      >
        <div className="flex items-center gap-2 border-b px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium text-foreground">{inst?.title || inst?.defKey || '…'}</div>
            <div className="truncate text-xs text-muted-foreground">
              {inst
                ? `${t('plugin_workflow.submitter')}: ${names(inst.submitter) || t('plugin_workflow.unknownUser')} · ${t('plugin_workflow.defVersion')} v${inst.defVersion}`
                : ''}
            </div>
          </div>
          {inst ? <Badge variant={statusVariant(inst.status)}>{t(`plugin_workflow.status.${inst.status}`)}</Badge> : null}
          <Button variant="ghost" size="icon" aria-label={t('common.close')} onClick={onClose}>
            <XCircle aria-hidden="true" />
          </Button>
        </div>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
          {inst?.definition ? (
            <section className="space-y-2">
              <h3 className="text-xs font-medium text-muted-foreground">{t('plugin_workflow.flowTitle')}</h3>
              <InstanceFlow definition={inst.definition} current={inst.current} done={inst.done} />
            </section>
          ) : null}

          {inst && inst.pending.length > 0 ? (
            <section className="space-y-2">
              <h3 className="text-xs font-medium text-muted-foreground">{t('plugin_workflow.pendingTitle')}</h3>
              <div className="flex flex-wrap gap-1.5">
                {[...new Set(inst.pending.map((p) => `${p.nodeName}\u0000${p.assignee}`))].map((sig) => {
                  const [nodeName, assignee] = sig.split('\u0000')
                  return (
                    <Badge key={sig} variant="secondary">
                      {nodeName} · {assignee ? names(assignee) || t('plugin_workflow.unknownUser') : t('plugin_workflow.anyone')}
                    </Badge>
                  )
                })}
              </div>
            </section>
          ) : null}

          {/* 申请内容（流程变量）：看不到内容就批是盲批；空值变量不上屏 */}
          {inst && Object.entries(inst.vars ?? {}).some(([, v]) => (v ?? '') !== '') ? (
            <section className="space-y-2">
              <h3 className="text-xs font-medium text-muted-foreground">{t('plugin_workflow.varsTitle')}</h3>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(inst.vars)
                  .filter(([, v]) => (v ?? '') !== '')
                  .map(([k, v]) => (
                    <Badge key={k} variant="outline" className="max-w-full gap-1 font-normal">
                      <span className="shrink-0 text-muted-foreground">{k}</span>
                      <span className="truncate">{v}</span>
                    </Badge>
                  ))}
              </div>
            </section>
          ) : null}

          <section className="space-y-2">
            <h3 className="text-xs font-medium text-muted-foreground">{t('plugin_workflow.historyTitle')}</h3>
            {inst?.history.map((e, i) => (
              <div key={i} className="flex gap-2 text-sm">
                <div className="mt-1 size-2 shrink-0 rounded-full bg-muted-foreground/40" />
                <div className="min-w-0 flex-1">
                  <div className="text-foreground">
                    <span className="font-medium">{names(e.actor) || t('plugin_workflow.unknownUser')}</span>{' '}
                    <span className="text-muted-foreground">{t(`plugin_workflow.actions.${e.action}`, { defaultValue: e.action })}</span>
                    <span className="text-muted-foreground"> · {e.node}</span>
                  </div>
                  {e.comment ? <div className="text-xs text-muted-foreground">{e.comment}</div> : null}
                  <div className="text-[11px] text-muted-foreground">{formatTime(e.at, i18n.language)}</div>
                </div>
              </div>
            ))}
            {!detail.isLoading && (inst?.history.length ?? 0) === 0 ? (
              <EmptyState title={t('plugin_workflow.empty')} className="py-8" />
            ) : null}
          </section>
        </div>

        {inst && isRunning ? (
          <div className="border-t p-3">
            {/* 驳回时必填：星号常驻但随驳回可用性提示（「驳回需要填写意见」在下方） */}
            <label className="mb-2 flex flex-col gap-1">
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                {t('plugin_workflow.commentPlaceholder')}
                <RequiredMark />
              </span>
              <Input
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder={t('plugin_workflow.commentPlaceholder')}
                aria-label={t('plugin_workflow.commentPlaceholder')}
              />
            </label>
            <Can perm="workflow:approve">
              <div className="flex flex-wrap items-center gap-2">
                <Button size="sm" onClick={() => void doAction('approve')} disabled={action.isPending || !myTask}>
                  <CircleCheck aria-hidden="true" />
                  {t('plugin_workflow.approve')}
                </Button>
                {/* 驳回必须给理由：没填意见就不给点 */}
                <Button
                  size="sm"
                  variant="outline"
                  className="text-destructive"
                  onClick={() => void doAction('reject', t('plugin_workflow.rejectConfirm'))}
                  disabled={action.isPending || !myTask || !comment.trim()}
                >
                  <XCircle aria-hidden="true" />
                  {t('plugin_workflow.reject')}
                </Button>
                <Button size="sm" variant="outline" onClick={() => setCountersignOpen((v) => !v)} disabled={action.isPending || !myTask}>
                  <UserPlus aria-hidden="true" />
                  {t('plugin_workflow.countersign')}
                </Button>
                <Button size="sm" variant="outline" onClick={() => setTransferOpen((v) => !v)} disabled={action.isPending || !myTask}>
                  <ArrowRightLeft aria-hidden="true" />
                  {t('plugin_workflow.transfer')}
                </Button>
                <Button size="sm" variant="outline" onClick={() => void doAction('escalate', t('plugin_workflow.escalateConfirm'))} disabled={action.isPending || !myTask}>
                  <Undo2 aria-hidden="true" />
                  {t('plugin_workflow.escalate')}
                </Button>
                <div className="flex items-center gap-1">
                  <SimpleSelect
                    className="w-32"
                    aria-label={t('plugin_workflow.rollbackTo')}
                    value={rollbackTo || NONE}
                    onChange={(v) => setRollbackTo(v === NONE ? '' : v)}
                    options={[
                      { value: NONE, label: t('plugin_workflow.rollbackTo') },
                      ...approvalNodes.map((n) => ({ value: n.key, label: n.name || n.key })),
                    ]}
                  />
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={action.isPending || !rollbackTo}
                    onClick={() => void doAction('rollback', t('plugin_workflow.rollbackConfirm'))}
                  >
                    {t('plugin_workflow.rollback')}
                  </Button>
                </div>
              </div>
              {!myTask ? <p className="mt-2 text-xs text-muted-foreground">{t('plugin_workflow.notMyTurn')}</p> : null}
              {myTask && !comment.trim() ? (
                <p className="mt-2 text-xs text-muted-foreground">{t('plugin_workflow.rejectNeedsComment')}</p>
              ) : null}
              {countersignOpen ? (
                <div className="mt-2 space-y-2 rounded border bg-muted/20 p-2">
                  {/* 会签/转办共用选人组件：「至少选一个」的组必填，星号标在选择器上方 */}
                  <p className="flex items-center gap-1 text-xs text-muted-foreground">
                    {t('plugin_workflow.countersign')}
                    <RequiredMark />
                  </p>
                  <ApproverPicker value={countersignUsers} onChange={setCountersignUsers} />
                  <div className="flex justify-end gap-2">
                    <Button size="sm" variant="ghost" onClick={() => setCountersignOpen(false)}>
                      {t('common.cancel')}
                    </Button>
                    <Button size="sm" onClick={() => void doCountersign()} disabled={action.isPending || countersignUsers.length === 0}>
                      {t('plugin_workflow.countersignSubmit', { count: countersignUsers.length })}
                    </Button>
                  </div>
                </div>
              ) : null}
              {transferOpen ? (
                <div className="mt-2 space-y-2 rounded border bg-muted/20 p-2">
                  <p className="flex items-center gap-1 text-xs text-muted-foreground">
                    {t('plugin_workflow.transfer')}
                    <RequiredMark />
                  </p>
                  <ApproverPicker value={transferTo} onChange={(v) => setTransferTo(v.slice(0, 1))} />
                  <div className="flex justify-end gap-2">
                    <Button size="sm" variant="ghost" onClick={() => setTransferOpen(false)}>
                      {t('common.cancel')}
                    </Button>
                    <Button size="sm" onClick={() => void doTransfer()} disabled={action.isPending || transferTo.length === 0}>
                      {t('plugin_workflow.transferSubmit')}
                    </Button>
                  </div>
                </div>
              ) : null}
            </Can>
            {canWithdraw ? (
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <Can perm="workflow:start">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void onUrge()}
                    disabled={urge.isPending}
                  >
                    <BellRing aria-hidden="true" />
                    {t('plugin_workflow.urge')}
                  </Button>
                </Can>
                <Can perm="workflow:start">
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-destructive"
                    onClick={() => void doAction('withdraw', t('plugin_workflow.withdrawConfirm'))}
                    disabled={action.isPending}
                  >
                    <GitBranch aria-hidden="true" />
                    {t('plugin_workflow.withdraw')}
                  </Button>
                </Can>
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  )
}
